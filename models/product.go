package models

import "time"

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

// 更新产品。库存数量变化统一记调整流水，成本仍由实际入库流水维护。
func UpdateProduct(id int, name, spec, unit, packaging string, stock int, price float64) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var currentStock, avgCost float64
	if err := tx.QueryRow("SELECT stock, avg_cost FROM products WHERE id = ? FOR UPDATE", id).Scan(&currentStock, &avgCost); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		UPDATE products
		SET name = ?, spec = ?, unit = ?, packaging = ?, price = ?
		WHERE id = ?
	`, name, spec, unit, packaging, price, id); err != nil {
		return err
	}
	delta := float64(stock) - currentStock
	if delta != 0 {
		if _, err := ApplyStockDeltaTx(tx, StockMovementInput{
			ItemType:      InventoryItemProduct,
			ItemID:        id,
			Quantity:      delta,
			UnitCost:      avgCost,
			MovementType:  MovementAdjustment,
			ReferenceType: "product_edit",
			ReferenceID:   int64(id),
			Remark:        "产品资料编辑中的库存调整",
		}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// 更新产品库存（保留兼容入口，实际写入调整流水）。
func UpdateProductStock(id int, newStock int) error {
	product, err := GetProductByID(id)
	if err != nil {
		return err
	}
	return UpdateProduct(id, product.Name, product.Spec, product.Unit, product.Packaging, newStock, product.Price)
}

// 删除产品
func DeleteProduct(id int) error {
	_, err := DB.Exec("DELETE FROM products WHERE id = ?", id)
	return err
}
