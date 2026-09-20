package models

import (
	"fmt"
	"time"
)

type Product struct {
	ID             int       `json:"id"`
	Name           string    `json:"name"`
	Spec           string    `json:"spec"`
	Unit           string    `json:"unit"`
	Packaging      string    `json:"packaging"`
	Stock          float64   `json:"stock"`
	ReservedStock  float64   `json:"reserved_stock"`
	AvailableStock float64   `json:"available_stock"`
	AvgCost        float64   `json:"avg_cost"`
	Price          float64   `json:"price"`
	CreatedAt      time.Time `json:"created_at"`
}

const productSelect = `
	SELECT p.id, p.name, p.spec, p.unit, p.packaging, p.stock,
	       COALESCE(r.reserved, 0) AS reserved_stock,
	       p.stock - COALESCE(r.reserved, 0) AS available_stock,
	       p.avg_cost, p.price, p.created_at
	FROM products p
	LEFT JOIN (
		SELECT item_id, SUM(quantity - consumed_quantity - released_quantity) AS reserved
		FROM inventory_reservations
		WHERE item_type = 'product' AND status = 'active'
		GROUP BY item_id
	) r ON r.item_id = p.id`

func scanProduct(scanner interface {
	Scan(dest ...interface{}) error
}) (*Product, error) {
	var p Product
	err := scanner.Scan(&p.ID, &p.Name, &p.Spec, &p.Unit, &p.Packaging, &p.Stock,
		&p.ReservedStock, &p.AvailableStock, &p.AvgCost, &p.Price, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// 查询所有产品
func GetAllProducts() ([]Product, error) {
	rows, err := DB.Query(productSelect + " ORDER BY p.id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var products []Product
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		products = append(products, *p)
	}
	return products, rows.Err()
}

// 获取单个产品
func GetProductByID(id int) (*Product, error) {
	return scanProduct(DB.QueryRow(productSelect+" WHERE p.id = ?", id))
}

// 添加产品。初始库存写入期初流水，不直接把编辑动作伪装成无来源库存。
func AddProduct(name, spec, unit, packaging string, stock int, price float64) (int64, error) {
	tx, err := DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	result, err := tx.Exec(`
		INSERT INTO products (name, spec, unit, packaging, stock, avg_cost, price)
		VALUES (?, ?, ?, ?, 0, 0, ?)
	`, name, spec, unit, packaging, price)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	if stock > 0 {
		if _, err := ApplyStockDeltaTx(tx, StockMovementInput{
			ItemType:      InventoryItemProduct,
			ItemID:        int(id),
			Quantity:      float64(stock),
			UnitCost:      0,
			MovementType:  MovementOpening,
			ReferenceType: "product_create",
			ReferenceID:   id,
			Remark:        "新建产品期初库存",
		}); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

// 更新产品资料。库存不属于资料字段，必须通过生产、出库或盘点改变。
func UpdateProduct(id int, name, spec, unit, packaging string, _ int, price float64) error {
	_, err := DB.Exec(`
		UPDATE products
		SET name = ?, spec = ?, unit = ?, packaging = ?, price = ?
		WHERE id = ?
	`, name, spec, unit, packaging, price, id)
	return err
}

// UpdateProductStock 已停用资料编辑直接改库存的旧入口。
func UpdateProductStock(_ int, _ int) error {
	return fmt.Errorf("库存不能通过编辑资料修改，请使用生产入库、出库或库存盘点")
}

// 删除产品
func DeleteProduct(id int) error {
	_, err := DB.Exec("DELETE FROM products WHERE id = ?", id)
	return err
}
