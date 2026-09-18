package models

import (
	"database/sql"
	"time"
)

type ProductBOM struct {
	ID            int       `json:"id"`
	ProductID     int       `json:"product_id"`
	RawMaterialID int       `json:"raw_material_id"`
	Quantity      float64   `json:"quantity"`
	CreatedAt     time.Time `json:"created_at"`
}

// BOMItem 用于BOM操作
type BOMItem struct {
	RawMaterialID int
	Quantity      float64
}

// GetBOMByProduct 获取产品的BOM清单
func GetBOMByProduct(productID int) ([]ProductBOM, error) {
	rows, err := DB.Query(`
        SELECT id, product_id, raw_material_id, quantity, created_at
        FROM product_bom
        WHERE product_id = ?
    `, productID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var boms []ProductBOM
	for rows.Next() {
		var b ProductBOM
		err := rows.Scan(&b.ID, &b.ProductID, &b.RawMaterialID, &b.Quantity, &b.CreatedAt)
		if err != nil {
			return nil, err
		}
		boms = append(boms, b)
	}
	return boms, nil
}

// ReplaceBOM 替换产品的BOM（先删后增）
func ReplaceBOM(productID int, bomItems []BOMItem) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec("DELETE FROM product_bom WHERE product_id = ?", productID)
	if err != nil {
		return err
	}

	for _, item := range bomItems {
		_, err = tx.Exec(`
            INSERT INTO product_bom (product_id, raw_material_id, quantity)
            VALUES (?, ?, ?)
        `, productID, item.RawMaterialID, item.Quantity)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// OrderBOMSnapshot 订单BOM快照：下单时锁定各产品的 BOM 用量，
// 供历史订单追溯，当前生产采购需求按实际BOM计算，
// 避免之后修改产品 BOM 影响历史订单。
type OrderBOMSnapshot struct {
	ID            int       `json:"id"`
	OrderID       int       `json:"order_id"`
	ProductID     int       `json:"product_id"`
	RawMaterialID int       `json:"raw_material_id"`
	Quantity      float64   `json:"quantity"`
	CreatedAt     time.Time `json:"created_at"`
}

// EnsureOrderBOMSnapshotTable 创建订单BOM快照表（如果不存在）。
func EnsureOrderBOMSnapshotTable() error {
	_, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS order_bom_snapshot (
			id              INT AUTO_INCREMENT PRIMARY KEY COMMENT '快照ID',
			order_id        INT NOT NULL COMMENT '订单ID',
			product_id      INT NOT NULL COMMENT '产品ID',
			raw_material_id INT NOT NULL COMMENT '原材料ID',
			quantity        DECIMAL(10,3) NOT NULL COMMENT '该订单需耗用的原材料数量（按下单时BOM计算）',
			created_at      DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
			KEY idx_order_bom_snapshot_order_id (order_id),
			FOREIGN KEY (order_id)        REFERENCES orders(id) ON DELETE CASCADE,
			FOREIGN KEY (product_id)      REFERENCES products(id) ON DELETE CASCADE,
			FOREIGN KEY (raw_material_id) REFERENCES raw_materials(id) ON DELETE CASCADE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='订单BOM快照表'
	`)
	return err
}

// GetOrderBOMSnapshot 读取指定订单的 BOM 快照（在调用方事务内执行，保证一致性）。
func GetOrderBOMSnapshot(tx *sql.Tx, orderID int) ([]OrderBOMSnapshot, error) {
	rows, err := tx.Query(`
		SELECT id, order_id, product_id, raw_material_id, quantity, created_at
		FROM order_bom_snapshot
		WHERE order_id = ?
	`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snapshots []OrderBOMSnapshot
	for rows.Next() {
		var s OrderBOMSnapshot
		if err := rows.Scan(&s.ID, &s.OrderID, &s.ProductID, &s.RawMaterialID, &s.Quantity, &s.CreatedAt); err != nil {
			return nil, err
		}
		snapshots = append(snapshots, s)
	}
	return snapshots, rows.Err()
}
