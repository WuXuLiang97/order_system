package models

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

// PurchaseMaterial 采购物料
type PurchaseMaterial struct {
	ID                  int        `json:"id"`
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
	Status              int        `json:"status"`
	Remark              string     `json:"remark"`
	PaymentReceipts     []string   `json:"payment_receipts"`
	StockAdded          int        `json:"stock_added"`
	CreatedAt           time.Time  `json:"created_at"`
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

// EnsurePurchaseMaterialsTable 创建采购物料表（如果不存在）
func EnsurePurchaseMaterialsTable() error {
	_, err := DB.Exec(`
        CREATE TABLE IF NOT EXISTS purchase_materials (
            id                    INT AUTO_INCREMENT PRIMARY KEY COMMENT '采购物料ID',
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
            status                TINYINT NOT NULL DEFAULT 0 COMMENT '0-采购中 1-已到货',
            remark                TEXT COMMENT '备注',
            payment_receipt       TEXT COMMENT '支付水单图片路径(JSON数组)',
            stock_added           TINYINT NOT NULL DEFAULT 0 COMMENT '是否已加入原材料库存',
            created_at            DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间'
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='采购物料表'
    `)
	if err != nil {
		return err
	}
	return ensurePaymentReceiptColumnText()
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

// GetAllPurchaseMaterials 获取所有采购物料
func GetAllPurchaseMaterials() ([]PurchaseMaterial, error) {
	rows, err := DB.Query(`
        SELECT id, material_name, material_type, spec, unit, quantity, price, amount,
               supplier, freight, purchase_date, expected_arrival_date, status,
               COALESCE(remark, '') AS remark, COALESCE(payment_receipt, '') AS payment_receipt,
               stock_added, created_at
        FROM purchase_materials
        ORDER BY id DESC
    `)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []PurchaseMaterial
	for rows.Next() {
		var p PurchaseMaterial
		var purchaseDate sql.NullTime
		var expectedDate sql.NullTime
		var paymentReceiptRaw string
		err := rows.Scan(&p.ID, &p.MaterialName, &p.MaterialType, &p.Spec, &p.Unit,
			&p.Quantity, &p.Price, &p.Amount, &p.Supplier, &p.Freight,
			&purchaseDate, &expectedDate, &p.Status, &p.Remark, &paymentReceiptRaw,
			&p.StockAdded, &p.CreatedAt)
		if err != nil {
			return nil, err
		}
		if purchaseDate.Valid {
			p.PurchaseDate = &purchaseDate.Time
		}
		if expectedDate.Valid {
			p.ExpectedArrivalDate = &expectedDate.Time
		}
		p.PaymentReceipts = parsePaymentReceipts(paymentReceiptRaw)
		list = append(list, p)
	}
	return list, nil
}

// GetPurchaseMaterialByID 获取单个采购物料
func GetPurchaseMaterialByID(id int) (*PurchaseMaterial, error) {
	var p PurchaseMaterial
	var purchaseDate sql.NullTime
	var expectedDate sql.NullTime
	var paymentReceiptRaw string
	err := DB.QueryRow(`
        SELECT id, material_name, material_type, spec, unit, quantity, price, amount,
               supplier, freight, purchase_date, expected_arrival_date, status,
               COALESCE(remark, '') AS remark, COALESCE(payment_receipt, '') AS payment_receipt,
               stock_added, created_at
        FROM purchase_materials
        WHERE id = ?
    `, id).Scan(&p.ID, &p.MaterialName, &p.MaterialType, &p.Spec, &p.Unit,
		&p.Quantity, &p.Price, &p.Amount, &p.Supplier, &p.Freight,
		&purchaseDate, &expectedDate, &p.Status, &p.Remark, &paymentReceiptRaw,
		&p.StockAdded, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	if purchaseDate.Valid {
		p.PurchaseDate = &purchaseDate.Time
	}
	if expectedDate.Valid {
		p.ExpectedArrivalDate = &expectedDate.Time
	}
	p.PaymentReceipts = parsePaymentReceipts(paymentReceiptRaw)
	return &p, nil
}

// DeletePurchaseMaterial 删除采购物料
func DeletePurchaseMaterial(id int) error {
	_, err := DB.Exec("DELETE FROM purchase_materials WHERE id = ?", id)
	return err
}
