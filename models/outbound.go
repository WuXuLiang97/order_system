package models

import (
	"database/sql"
	"time"
)

// ============ 成品出库（送货单） ============

type ProductOutbound struct {
	ID               int       `json:"id"`
	OutboundNo       string    `json:"outbound_no"`
	OutDate          time.Time `json:"out_date"`
	Receiver         string    `json:"receiver"`
	SettlementMethod string    `json:"settlement_method"`
	OrderID          int       `json:"order_id"`
	OrderNo          string    `json:"order_no"`
	Remark           string    `json:"remark"`
	CreatedByUserID  int       `json:"created_by_user_id"`
	CreatedByName    string    `json:"created_by_name"`
	TotalQuantity    float64   `json:"total_quantity"`
	ItemCount        int       `json:"item_count"`
	CreatedAt        time.Time `json:"created_at"`
}

type ProductOutboundItem struct {
	ID              int     `json:"id"`
	OutboundID      int     `json:"outbound_id"`
	OrderItemID     int     `json:"order_item_id"`
	ProductID       int     `json:"product_id"`
	ProductName     string  `json:"product_name"`
	Spec            string  `json:"spec"`
	Unit            string  `json:"unit"`
	Quantity        float64 `json:"quantity"`
	OrderedQuantity float64 `json:"ordered_quantity"`
}

// EnsureProductOutboundTables 创建成品出库主表与明细表。
func EnsureProductOutboundTables() error {
	if _, err := DB.Exec(`
        CREATE TABLE IF NOT EXISTS product_outbound (
            id                 INT AUTO_INCREMENT PRIMARY KEY COMMENT '出库ID',
            outbound_no        VARCHAR(32) NOT NULL DEFAULT '' COMMENT '送货单号',
            out_date           DATE NULL COMMENT '出库日期',
            receiver           VARCHAR(200) NOT NULL DEFAULT '' COMMENT '客户名称',
            settlement_method  VARCHAR(20) NOT NULL DEFAULT '现金' COMMENT '结算方式：现金/到付',
            order_id           INT NOT NULL DEFAULT 0 COMMENT '关联订单ID',
            order_no           VARCHAR(32) NOT NULL DEFAULT '' COMMENT '订单号快照',
            remark             VARCHAR(500) NOT NULL DEFAULT '' COMMENT '备注',
            created_by_user_id INT NOT NULL DEFAULT 0 COMMENT '操作人用户ID',
            created_at         DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
            KEY idx_outbound_no (outbound_no),
            KEY idx_outbound_order_id (order_id)
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='成品出库表'
    `); err != nil {
		return err
	}
	if err := ensureColumn("product_outbound", "settlement_method", "VARCHAR(20) NOT NULL DEFAULT '现金' COMMENT '结算方式：现金/到付'"); err != nil {
		return err
	}
	if err := ensureColumn("product_outbound", "order_id", "INT NOT NULL DEFAULT 0 COMMENT '关联订单ID'"); err != nil {
		return err
	}
	if err := ensureColumn("product_outbound", "order_no", "VARCHAR(32) NOT NULL DEFAULT '' COMMENT '订单号快照'"); err != nil {
		return err
	}
	if err := ensureIndex("product_outbound", "idx_outbound_order_id", "order_id"); err != nil {
		return err
	}

	if _, err := DB.Exec(`
        CREATE TABLE IF NOT EXISTS product_outbound_items (
            id               INT AUTO_INCREMENT PRIMARY KEY COMMENT '明细ID',
            outbound_id      INT NOT NULL COMMENT '出库ID',
            order_item_id    INT NOT NULL DEFAULT 0 COMMENT '关联订单明细ID',
            product_id       INT NOT NULL COMMENT '产品ID',
            product_name     VARCHAR(200) NOT NULL DEFAULT '' COMMENT '产品名称(快照)',
            spec             VARCHAR(100) NOT NULL DEFAULT '' COMMENT '规格型号(快照)',
            unit             VARCHAR(20) NOT NULL DEFAULT '' COMMENT '单位(快照)',
            quantity         DECIMAL(12,3) NOT NULL DEFAULT 0 COMMENT '出库数量',
            ordered_quantity DECIMAL(12,3) NOT NULL DEFAULT 0 COMMENT '订单数量快照',
            KEY idx_outbound (outbound_id),
            KEY idx_outbound_item_order_item (order_item_id)
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='成品出库明细表'
    `); err != nil {
		return err
	}
	if err := ensureColumn("product_outbound_items", "order_item_id", "INT NOT NULL DEFAULT 0 COMMENT '关联订单明细ID'"); err != nil {
		return err
	}
	if err := ensureColumn("product_outbound_items", "ordered_quantity", "DECIMAL(12,3) NOT NULL DEFAULT 0 COMMENT '订单数量快照'"); err != nil {
		return err
	}
	return ensureIndex("product_outbound_items", "idx_outbound_item_order_item", "order_item_id")
}

// ListProductOutbounds 出库记录列表（含明细汇总与操作人）。
func ListProductOutbounds(limit int) ([]ProductOutbound, error) {
	rows, err := DB.Query(`
        SELECT o.id, o.outbound_no, o.out_date, o.receiver,
               COALESCE(NULLIF(o.settlement_method, ''), '现金') AS settlement_method,
               o.order_id, o.order_no, o.remark, o.created_by_user_id,
               COALESCE(NULLIF(u.display_name, ''), u.username, '') AS created_by_name,
               o.created_at, COALESCE(SUM(i.quantity), 0) AS total_quantity, COUNT(i.id) AS item_count
        FROM product_outbound o
        LEFT JOIN product_outbound_items i ON i.outbound_id = o.id
        LEFT JOIN users u ON u.id = o.created_by_user_id
        GROUP BY o.id, o.outbound_no, o.out_date, o.receiver, o.settlement_method,
                 o.order_id, o.order_no, o.remark, o.created_by_user_id, o.created_at
        ORDER BY o.id DESC
        LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []ProductOutbound
	for rows.Next() {
		var ob ProductOutbound
		var outDate sql.NullTime
		if err := rows.Scan(&ob.ID, &ob.OutboundNo, &outDate, &ob.Receiver, &ob.SettlementMethod,
			&ob.OrderID, &ob.OrderNo, &ob.Remark, &ob.CreatedByUserID, &ob.CreatedByName,
			&ob.CreatedAt, &ob.TotalQuantity, &ob.ItemCount); err != nil {
			return nil, err
		}
		if outDate.Valid {
			ob.OutDate = outDate.Time
		}
		list = append(list, ob)
	}
	return list, rows.Err()
}

// GetProductOutboundsByOrder 获取某订单关联的全部送货单。
func GetProductOutboundsByOrder(orderID int) ([]ProductOutbound, error) {
	rows, err := DB.Query(`
        SELECT o.id, o.outbound_no, o.out_date, o.receiver,
               COALESCE(NULLIF(o.settlement_method, ''), '现金') AS settlement_method,
               o.order_id, o.order_no, o.remark, o.created_by_user_id,
               COALESCE(NULLIF(u.display_name, ''), u.username, '') AS created_by_name,
               o.created_at, COALESCE(SUM(i.quantity), 0) AS total_quantity, COUNT(i.id) AS item_count
        FROM product_outbound o
        LEFT JOIN product_outbound_items i ON i.outbound_id = o.id
        LEFT JOIN users u ON u.id = o.created_by_user_id
        WHERE o.order_id = ?
        GROUP BY o.id, o.outbound_no, o.out_date, o.receiver, o.settlement_method,
                 o.order_id, o.order_no, o.remark, o.created_by_user_id, o.created_at
        ORDER BY o.id DESC`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []ProductOutbound
	for rows.Next() {
		var ob ProductOutbound
		var outDate sql.NullTime
		if err := rows.Scan(&ob.ID, &ob.OutboundNo, &outDate, &ob.Receiver, &ob.SettlementMethod,
			&ob.OrderID, &ob.OrderNo, &ob.Remark, &ob.CreatedByUserID, &ob.CreatedByName,
			&ob.CreatedAt, &ob.TotalQuantity, &ob.ItemCount); err != nil {
			return nil, err
		}
		if outDate.Valid {
			ob.OutDate = outDate.Time
		}
		list = append(list, ob)
	}
	return list, rows.Err()
}

// GetProductOutboundItems 获取某张送货单的明细。
func GetProductOutboundItems(outboundID int) ([]ProductOutboundItem, error) {
	rows, err := DB.Query(`
        SELECT id, outbound_id, order_item_id, product_id, product_name, spec, unit, quantity, ordered_quantity
        FROM product_outbound_items
        WHERE outbound_id = ?
        ORDER BY id ASC`, outboundID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []ProductOutboundItem
	for rows.Next() {
		var it ProductOutboundItem
		if err := rows.Scan(&it.ID, &it.OutboundID, &it.OrderItemID, &it.ProductID, &it.ProductName,
			&it.Spec, &it.Unit, &it.Quantity, &it.OrderedQuantity); err != nil {
			return nil, err
		}
		list = append(list, it)
	}
	return list, rows.Err()
}

// GetProductOutboundByNo 按送货单号查询主记录（打印页标题等）。
func GetProductOutboundByNo(outboundNo string) (*ProductOutbound, error) {
	var ob ProductOutbound
	var outDate sql.NullTime
	err := DB.QueryRow(`
        SELECT o.id, o.outbound_no, o.out_date, o.receiver,
               COALESCE(NULLIF(o.settlement_method, ''), '现金') AS settlement_method,
               o.order_id, o.order_no, o.remark, o.created_by_user_id,
               COALESCE(NULLIF(u.display_name, ''), u.username, '') AS created_by_name,
               o.created_at, 0 AS total_quantity, 0 AS item_count
        FROM product_outbound o
        LEFT JOIN users u ON u.id = o.created_by_user_id
        WHERE o.outbound_no = ?`, outboundNo).
		Scan(&ob.ID, &ob.OutboundNo, &outDate, &ob.Receiver, &ob.SettlementMethod,
			&ob.OrderID, &ob.OrderNo, &ob.Remark, &ob.CreatedByUserID, &ob.CreatedByName,
			&ob.CreatedAt, &ob.TotalQuantity, &ob.ItemCount)
	if err != nil {
		return nil, err
	}
	if outDate.Valid {
		ob.OutDate = outDate.Time
	}
	return &ob, nil
}
