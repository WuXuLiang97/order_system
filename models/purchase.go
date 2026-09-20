package models

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// PurchaseMaterial 采购单中的一条物料明细。
type PurchaseMaterial struct {
	ID                  int        `json:"id"`
	PurchaseOrderID     int        `json:"purchase_order_id"`
	RawMaterialID       int        `json:"raw_material_id"`
	MaterialName        string     `json:"material_name"`
	MaterialType        string     `json:"material_type"`
	Spec                string     `json:"spec"`
	Unit                string     `json:"unit"`
	Quantity            float64    `json:"quantity"`
	Price               float64    `json:"price"`
	Amount              float64    `json:"amount"`
	Supplier            string     `json:"supplier"`
	Freight             float64    `json:"freight"`
	PurchaseDate        *time.Time `json:"purchase_date"`
	ExpectedArrivalDate *time.Time `json:"expected_arrival_date"`
	ActualArrivalDate   *time.Time `json:"actual_arrival_date"`
	Status              int        `json:"status"`
	PaymentStatus       string     `json:"payment_status"`
	Remark              string     `json:"remark"`
	PaymentReceipts     []string   `json:"payment_receipts"`
	StockAdded          int        `json:"stock_added"`
	CreatedAt           time.Time  `json:"created_at"`
}

// PurchaseOrder 采购单主表，一张采购单可以包含多条物料明细。
type PurchaseOrder struct {
	ID                  int                `json:"id"`
	PurchaseNo          string             `json:"purchase_no"`
	Supplier            string             `json:"supplier"`
	Freight             float64            `json:"freight"`
	PurchaseDate        *time.Time         `json:"purchase_date"`
	ExpectedArrivalDate *time.Time         `json:"expected_arrival_date"`
	ActualArrivalDate   *time.Time         `json:"actual_arrival_date"`
	PaymentStatus       string             `json:"payment_status"`
	Status              int                `json:"status"`
	Remark              string             `json:"remark"`
	PaymentReceipts     []string           `json:"payment_receipts"`
	CreatedAt           time.Time          `json:"created_at"`
	Items               []PurchaseMaterial `json:"items"`
	TotalAmount         float64            `json:"total_amount"`
	TotalQuantity       float64            `json:"total_quantity"`
	ItemCount           int                `json:"item_count"`
}

func parsePaymentReceipts(raw string) []string {
	if raw == "" {
		return []string{}
	}
	var urls []string
	if err := json.Unmarshal([]byte(raw), &urls); err != nil {
		return []string{}
	}
	if urls == nil {
		return []string{}
	}
	return urls
}

// EnsurePurchaseMaterialsTable 创建采购物料明细表（如果不存在）。
func EnsurePurchaseMaterialsTable() error {
	_, err := DB.Exec(`
        CREATE TABLE IF NOT EXISTS purchase_materials (
            id                    INT AUTO_INCREMENT PRIMARY KEY COMMENT '采购物料明细ID',
            purchase_order_id      INT NULL COMMENT '采购单ID',
            raw_material_id       INT NOT NULL DEFAULT 0 COMMENT '关联原材料ID',
            material_name         VARCHAR(100) NOT NULL COMMENT '物料名称',
            material_type         VARCHAR(20)  NOT NULL DEFAULT '原材料' COMMENT '物料类型',
            spec                  VARCHAR(100) NOT NULL DEFAULT '' COMMENT '规格型号',
            unit                  VARCHAR(20)  NOT NULL DEFAULT '个' COMMENT '单位',
            quantity              DECIMAL(12,3) NOT NULL DEFAULT 0 COMMENT '采购数量',
            price                 DECIMAL(10,2) NOT NULL DEFAULT 0 COMMENT '单价',
            amount                DECIMAL(12,2) NOT NULL DEFAULT 0 COMMENT '金额',
            supplier              VARCHAR(100) NOT NULL DEFAULT '' COMMENT '供应商',
            freight               DECIMAL(10,2) NOT NULL DEFAULT 0 COMMENT '运费',
            purchase_date         DATE NULL COMMENT '采购日期',
            expected_arrival_date DATE NULL COMMENT '预计到货日期',
            actual_arrival_date   DATE NULL COMMENT '实际到货日期',
            status                TINYINT NOT NULL DEFAULT 0 COMMENT '0-采购中 1-已到货 2-草稿',
            payment_status        VARCHAR(20) NOT NULL DEFAULT '未付款' COMMENT '付款状态',
            remark                TEXT COMMENT '备注',
            payment_receipt       TEXT COMMENT '支付水单图片路径(JSON数组)',
            stock_added           TINYINT NOT NULL DEFAULT 0 COMMENT '是否已加入原材料库存',
            created_at            DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间'
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='采购物料明细表'
    `)
	if err != nil {
		return err
	}
	if err := ensureColumn("purchase_materials", "purchase_order_id", "INT NULL COMMENT '采购单ID'"); err != nil {
		return err
	}
	if err := ensureColumn("purchase_materials", "raw_material_id", "INT NOT NULL DEFAULT 0 COMMENT '关联原材料ID'"); err != nil {
		return err
	}
	if err := ensureColumn("purchase_materials", "actual_arrival_date", "DATE NULL"); err != nil {
		return err
	}
	if err := ensureColumn("purchase_materials", "payment_status", "VARCHAR(20) NOT NULL DEFAULT '未付款'"); err != nil {
		return err
	}
	if err := ensurePaymentReceiptColumnText(); err != nil {
		return err
	}
	if err := ensureIndex("purchase_materials", "idx_purchase_material_order_id", "purchase_order_id"); err != nil {
		return err
	}
	if err := ensureIndex("purchase_materials", "idx_purchase_material_raw_material_id", "raw_material_id"); err != nil {
		return err
	}
	return backfillPurchaseMaterialRawMaterialIDs()
}

// backfillPurchaseMaterialRawMaterialIDs 为升级前没有关联ID、且名称和规格唯一匹配的采购明细补齐原材料ID。
func backfillPurchaseMaterialRawMaterialIDs() error {
	_, err := DB.Exec(`
		UPDATE purchase_materials pm
		JOIN (
			SELECT name, COALESCE(spec, '') AS material_spec, MIN(id) AS raw_material_id
			FROM raw_materials
			GROUP BY name, COALESCE(spec, '')
			HAVING COUNT(*) = 1
		) rm ON pm.material_name = rm.name AND COALESCE(pm.spec, '') = rm.material_spec
		SET pm.raw_material_id = rm.raw_material_id
		WHERE pm.raw_material_id = 0
		  AND pm.material_type = '原材料'
	`)
	return err
}

// EnsurePurchaseOrdersTable 创建采购单主表，并把旧的单条采购记录迁移成单明细采购单。
func EnsurePurchaseOrdersTable() error {
	_, err := DB.Exec(`
        CREATE TABLE IF NOT EXISTS purchase_orders (
            id                    INT AUTO_INCREMENT PRIMARY KEY COMMENT '采购单ID',
            purchase_no           VARCHAR(32) NOT NULL UNIQUE COMMENT '采购单号',
            supplier              VARCHAR(100) NOT NULL DEFAULT '' COMMENT '供应商',
            freight               DECIMAL(10,2) NOT NULL DEFAULT 0 COMMENT '运费',
            purchase_date         DATE NULL COMMENT '采购日期',
            expected_arrival_date DATE NULL COMMENT '预计到货日期',
            actual_arrival_date   DATE NULL COMMENT '实际到货日期',
            payment_status        VARCHAR(20) NOT NULL DEFAULT '未付款' COMMENT '付款状态',
            status                TINYINT NOT NULL DEFAULT 0 COMMENT '0-采购中 1-已到货 2-草稿',
            remark                TEXT COMMENT '备注',
            payment_receipt       TEXT COMMENT '支付水单图片路径(JSON数组)',
            created_at            DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间'
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='采购单主表'
    `)
	if err != nil {
		return err
	}
	if _, err := DB.Exec(`
        CREATE TABLE IF NOT EXISTS purchase_order_sequences (
            purchase_date         DATE NOT NULL PRIMARY KEY COMMENT '采购日期',
            current_no            INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '当天已使用的最大序号'
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='采购单号按日序号表'
    `); err != nil {
		return err
	}
	return migrateLegacyPurchaseMaterials()
}

// migrateLegacyPurchaseMaterials 将新增 purchase_order_id 前的旧采购记录逐条迁移成采购单。
func migrateLegacyPurchaseMaterials() error {
	type legacyRow struct {
		ID                  int
		MaterialName        string
		MaterialType        string
		Spec                string
		Unit                string
		Quantity            float64
		Price               float64
		Amount              float64
		Supplier            string
		Freight             float64
		PurchaseDate        sql.NullTime
		ExpectedArrivalDate sql.NullTime
		ActualArrivalDate   sql.NullTime
		PaymentStatus       string
		Status              int
		Remark              string
		PaymentReceipt      string
		CreatedAt           time.Time
	}

	rows, err := DB.Query(`
        SELECT id, material_name, material_type, spec, unit, quantity, price, amount,
               supplier, freight, purchase_date, expected_arrival_date, actual_arrival_date,
               COALESCE(payment_status, '未付款'), status, COALESCE(remark, ''),
               COALESCE(payment_receipt, ''), created_at
        FROM purchase_materials
        WHERE purchase_order_id IS NULL OR purchase_order_id = 0
        ORDER BY id ASC
    `)
	if err != nil {
		return err
	}
	var legacy []legacyRow
	for rows.Next() {
		var item legacyRow
		if err := rows.Scan(
			&item.ID, &item.MaterialName, &item.MaterialType, &item.Spec, &item.Unit,
			&item.Quantity, &item.Price, &item.Amount, &item.Supplier, &item.Freight,
			&item.PurchaseDate, &item.ExpectedArrivalDate, &item.ActualArrivalDate,
			&item.PaymentStatus, &item.Status, &item.Remark, &item.PaymentReceipt, &item.CreatedAt,
		); err != nil {
			rows.Close()
			return err
		}
		legacy = append(legacy, item)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if len(legacy) == 0 {
		return nil
	}

	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, item := range legacy {
		numberDate := item.CreatedAt
		if item.PurchaseDate.Valid {
			numberDate = item.PurchaseDate.Time
		}
		purchaseNo, err := NextPurchaseNoTx(tx, numberDate)
		if err != nil {
			return err
		}
		result, err := tx.Exec(`
            INSERT INTO purchase_orders
                (purchase_no, supplier, freight, purchase_date, expected_arrival_date,
                 actual_arrival_date, payment_status, status, remark, payment_receipt, created_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        `, purchaseNo, item.Supplier, item.Freight, nullableTime(item.PurchaseDate), nullableTime(item.ExpectedArrivalDate),
			nullableTime(item.ActualArrivalDate), item.PaymentStatus, item.Status, item.Remark, item.PaymentReceipt, item.CreatedAt)
		if err != nil {
			return err
		}
		orderID, err := result.LastInsertId()
		if err != nil {
			return err
		}
		if _, err := tx.Exec("UPDATE purchase_materials SET purchase_order_id = ? WHERE id = ?", orderID, item.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func nullableTime(value sql.NullTime) interface{} {
	if !value.Valid {
		return nil
	}
	return value.Time
}

const (
	purchaseNoPrefix           = "CG"
	purchaseNoMaxDailySequence = 9999
)

// NextPurchaseNoTx 按采购日期生成“CG+YYYYMMDD+4位当天序号”，例如 CG202609190001。
// 序号表行锁保证同一日期并发新增时不会生成重复单号，并会兼容已有无前缀单号。
func NextPurchaseNoTx(tx *sql.Tx, purchaseDate time.Time) (string, error) {
	datePart := purchaseDate.Format("20060102")
	if _, err := tx.Exec(`
        INSERT INTO purchase_order_sequences (purchase_date, current_no)
        VALUES (?, 0)
        ON DUPLICATE KEY UPDATE current_no = current_no
    `, purchaseDate); err != nil {
		return "", err
	}

	var currentNo int
	if err := tx.QueryRow(`
        SELECT current_no
        FROM purchase_order_sequences
        WHERE purchase_date = ?
        FOR UPDATE
    `, purchaseDate).Scan(&currentNo); err != nil {
		return "", err
	}

	var existingMax int
	if err := tx.QueryRow(`
        SELECT COALESCE(MAX(CAST(RIGHT(purchase_no, 4) AS UNSIGNED)), 0)
        FROM purchase_orders
        WHERE (
                (CHAR_LENGTH(purchase_no) = 14 AND purchase_no REGEXP '^CG[0-9]{12}$')
             OR (CHAR_LENGTH(purchase_no) = 12 AND purchase_no REGEXP '^[0-9]{12}$')
            )
          AND SUBSTRING(purchase_no, IF(LEFT(purchase_no, 2) = 'CG', 3, 1), 8) = ?
    `, datePart).Scan(&existingMax); err != nil {
		return "", err
	}
	if existingMax > currentNo {
		currentNo = existingMax
	}
	if currentNo >= purchaseNoMaxDailySequence {
		return "", fmt.Errorf("当天采购单号已用完（最多%d单）", purchaseNoMaxDailySequence)
	}

	nextNo := currentNo + 1
	if _, err := tx.Exec(`
        UPDATE purchase_order_sequences
        SET current_no = ?
        WHERE purchase_date = ?
    `, nextNo, purchaseDate); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%s%04d", purchaseNoPrefix, datePart, nextNo), nil
}

// ensurePaymentReceiptColumnText 兼容旧版本：将支付水单字段从 VARCHAR 升级为 TEXT。
func ensurePaymentReceiptColumnText() error {
	var columnType string
	err := DB.QueryRow(`
        SELECT COLUMN_TYPE
        FROM information_schema.COLUMNS
        WHERE TABLE_SCHEMA = DATABASE()
          AND TABLE_NAME = 'purchase_materials'
          AND COLUMN_NAME = 'payment_receipt'
    `).Scan(&columnType)
	if err != nil {
		return err
	}
	if strings.Contains(strings.ToLower(columnType), "text") {
		return nil
	}
	_, err = DB.Exec("ALTER TABLE purchase_materials MODIFY COLUMN payment_receipt TEXT")
	return err
}

// GetAllPurchaseOrders 获取所有采购单，按采购日期倒序排列，最新采购单在最上方。
func GetAllPurchaseOrders() ([]PurchaseOrder, error) {
	rows, err := DB.Query(`
        SELECT po.id, po.purchase_no, po.supplier, po.freight, po.purchase_date,
               po.expected_arrival_date, po.actual_arrival_date, po.payment_status,
               po.status, COALESCE(po.remark, ''), COALESCE(po.payment_receipt, ''), po.created_at,
               COALESCE((SELECT SUM(pm.amount) FROM purchase_materials pm WHERE pm.purchase_order_id = po.id), 0),
               COALESCE((SELECT SUM(pm.quantity) FROM purchase_materials pm WHERE pm.purchase_order_id = po.id), 0),
               (SELECT COUNT(*) FROM purchase_materials pm WHERE pm.purchase_order_id = po.id)
        FROM purchase_orders po
        ORDER BY po.purchase_date DESC, po.id DESC
    `)
	if err != nil {
		return nil, err
	}
	var orders []PurchaseOrder
	for rows.Next() {
		order, err := scanPurchaseOrder(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		orders = append(orders, *order)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := attachPurchaseItems(orders); err != nil {
		return nil, err
	}
	return orders, nil
}

// GetPurchaseOrderByID 获取一张采购单及其全部物料明细。
func GetPurchaseOrderByID(id int) (*PurchaseOrder, error) {
	row := DB.QueryRow(`
        SELECT po.id, po.purchase_no, po.supplier, po.freight, po.purchase_date,
               po.expected_arrival_date, po.actual_arrival_date, po.payment_status,
               po.status, COALESCE(po.remark, ''), COALESCE(po.payment_receipt, ''), po.created_at,
               COALESCE((SELECT SUM(pm.amount) FROM purchase_materials pm WHERE pm.purchase_order_id = po.id), 0),
               COALESCE((SELECT SUM(pm.quantity) FROM purchase_materials pm WHERE pm.purchase_order_id = po.id), 0),
               (SELECT COUNT(*) FROM purchase_materials pm WHERE pm.purchase_order_id = po.id)
        FROM purchase_orders po
        WHERE po.id = ?
    `, id)
	order, err := scanPurchaseOrder(row)
	if err != nil {
		return nil, err
	}
	items, err := getPurchaseItemsByOrderID(id)
	if err != nil {
		return nil, err
	}
	order.Items = items
	return order, nil
}

type purchaseOrderScanner interface {
	Scan(dest ...interface{}) error
}

func scanPurchaseOrder(scanner purchaseOrderScanner) (*PurchaseOrder, error) {
	var order PurchaseOrder
	var purchaseDate, expectedDate, actualDate sql.NullTime
	var receiptRaw string
	if err := scanner.Scan(
		&order.ID, &order.PurchaseNo, &order.Supplier, &order.Freight, &purchaseDate,
		&expectedDate, &actualDate, &order.PaymentStatus, &order.Status, &order.Remark,
		&receiptRaw, &order.CreatedAt, &order.TotalAmount, &order.TotalQuantity, &order.ItemCount,
	); err != nil {
		return nil, err
	}
	if purchaseDate.Valid {
		order.PurchaseDate = &purchaseDate.Time
	}
	if expectedDate.Valid {
		order.ExpectedArrivalDate = &expectedDate.Time
	}
	if actualDate.Valid {
		order.ActualArrivalDate = &actualDate.Time
	}
	order.PaymentReceipts = parsePaymentReceipts(receiptRaw)
	order.Items = []PurchaseMaterial{}
	return &order, nil
}

func attachPurchaseItems(orders []PurchaseOrder) error {
	if len(orders) == 0 {
		return nil
	}
	rows, err := DB.Query(`
        SELECT pm.id, pm.purchase_order_id, pm.raw_material_id,
               COALESCE(rm.name, pm.material_name), pm.material_type,
               COALESCE(rm.spec, pm.spec), COALESCE(rm.unit, pm.unit),
               pm.quantity, pm.price, pm.amount, pm.supplier, pm.freight,
               pm.purchase_date, pm.expected_arrival_date, pm.actual_arrival_date,
               pm.payment_status, pm.status, COALESCE(pm.remark, ''),
               COALESCE(pm.payment_receipt, ''), pm.stock_added, pm.created_at
        FROM purchase_materials pm
        LEFT JOIN raw_materials rm ON rm.id = pm.raw_material_id
        WHERE pm.purchase_order_id IS NOT NULL
        ORDER BY pm.purchase_order_id ASC, pm.id ASC
    `)
	if err != nil {
		return err
	}
	defer rows.Close()

	itemsByOrder := make(map[int][]PurchaseMaterial)
	for rows.Next() {
		item, err := scanPurchaseMaterial(rows)
		if err != nil {
			return err
		}
		itemsByOrder[item.PurchaseOrderID] = append(itemsByOrder[item.PurchaseOrderID], *item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range orders {
		items := itemsByOrder[orders[i].ID]
		if items == nil {
			items = []PurchaseMaterial{}
		}
		orders[i].Items = items
	}
	return nil
}

func getPurchaseItemsByOrderID(orderID int) ([]PurchaseMaterial, error) {
	rows, err := DB.Query(`
        SELECT pm.id, pm.purchase_order_id, pm.raw_material_id,
               COALESCE(rm.name, pm.material_name), pm.material_type,
               COALESCE(rm.spec, pm.spec), COALESCE(rm.unit, pm.unit),
               pm.quantity, pm.price, pm.amount, pm.supplier, pm.freight,
               pm.purchase_date, pm.expected_arrival_date, pm.actual_arrival_date,
               pm.payment_status, pm.status, COALESCE(pm.remark, ''),
               COALESCE(pm.payment_receipt, ''), pm.stock_added, pm.created_at
        FROM purchase_materials pm
        LEFT JOIN raw_materials rm ON rm.id = pm.raw_material_id
        WHERE pm.purchase_order_id = ?
        ORDER BY pm.id ASC
    `, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []PurchaseMaterial{}
	for rows.Next() {
		item, err := scanPurchaseMaterial(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func scanPurchaseMaterial(scanner purchaseOrderScanner) (*PurchaseMaterial, error) {
	var item PurchaseMaterial
	var purchaseDate, expectedDate, actualDate sql.NullTime
	var receiptRaw string
	if err := scanner.Scan(
		&item.ID, &item.PurchaseOrderID, &item.RawMaterialID, &item.MaterialName, &item.MaterialType, &item.Spec, &item.Unit,
		&item.Quantity, &item.Price, &item.Amount, &item.Supplier, &item.Freight,
		&purchaseDate, &expectedDate, &actualDate, &item.PaymentStatus, &item.Status,
		&item.Remark, &receiptRaw, &item.StockAdded, &item.CreatedAt,
	); err != nil {
		return nil, err
	}
	if purchaseDate.Valid {
		item.PurchaseDate = &purchaseDate.Time
	}
	if expectedDate.Valid {
		item.ExpectedArrivalDate = &expectedDate.Time
	}
	if actualDate.Valid {
		item.ActualArrivalDate = &actualDate.Time
	}
	item.PaymentReceipts = parsePaymentReceipts(receiptRaw)
	return &item, nil
}

// DeletePurchaseOrder 删除一张采购单及其全部明细。
func DeletePurchaseOrder(id int) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	result, err := tx.Exec("DELETE FROM purchase_orders WHERE id = ?", id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	if _, err := tx.Exec("DELETE FROM purchase_materials WHERE purchase_order_id = ?", id); err != nil {
		return err
	}
	return tx.Commit()
}
