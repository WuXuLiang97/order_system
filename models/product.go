package models

import "time"

type Product struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	Spec      string    `json:"spec"`
	Unit      string    `json:"unit"`
	Stock     float64   `json:"stock"`
	Price     float64   `json:"price"`
	CreatedAt time.Time `json:"created_at"`
}

// 查询所有产品
func GetAllProducts() ([]Product, error) {
	rows, err := DB.Query("SELECT id, name, spec, unit, stock, price, created_at FROM products ORDER BY id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var products []Product
	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.Name, &p.Spec, &p.Unit, &p.Stock, &p.Price, &p.CreatedAt); err != nil {
			return nil, err
		}
		products = append(products, p)
	}
	return products, nil
}

// 获取单个产品
func GetProductByID(id int) (*Product, error) {
	var p Product
	err := DB.QueryRow("SELECT id, name, spec, unit, stock, price, created_at FROM products WHERE id = ?", id).
		Scan(&p.ID, &p.Name, &p.Spec, &p.Unit, &p.Stock, &p.Price, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// 添加产品
func AddProduct(name, spec, unit string, stock int, price float64) (int64, error) {
	result, err := DB.Exec("INSERT INTO products (name, spec, unit, stock, price) VALUES (?, ?, ?, ?, ?)", name, spec, unit, stock, price)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// 更新产品
func UpdateProduct(id int, name, spec, unit string, stock int, price float64) error {
	_, err := DB.Exec("UPDATE products SET name = ?, spec = ?, unit = ?, stock = ?, price = ? WHERE id = ?", name, spec, unit, stock, price, id)
	return err
}

// 更新产品库存（用于订单扣减）
func UpdateProductStock(id int, newStock int) error {
	_, err := DB.Exec("UPDATE products SET stock = ? WHERE id = ?", newStock, id)
	return err
}

// 删除产品
func DeleteProduct(id int) error {
	_, err := DB.Exec("DELETE FROM products WHERE id = ?", id)
	return err
}
