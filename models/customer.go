package models

import "time"

type Customer struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	Phone     string    `json:"phone"`
	Address   string    `json:"address"`
	CreatedAt time.Time `json:"created_at"`
}

func EnsureCustomersTable() error {
	_, err := DB.Exec(`
        CREATE TABLE IF NOT EXISTS customers (
            id          INT AUTO_INCREMENT PRIMARY KEY COMMENT '客户ID',
            name        VARCHAR(100) NOT NULL COMMENT '客户名称',
            phone       VARCHAR(20)  NOT NULL DEFAULT '' COMMENT '电话号码',
            address     VARCHAR(255) NOT NULL DEFAULT '' COMMENT '收货地址',
            created_at  DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间'
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='客户表'
    `)
	return err
}

func GetAllCustomers() ([]Customer, error) {
	rows, err := DB.Query("SELECT id, name, phone, address, created_at FROM customers ORDER BY id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Customer
	for rows.Next() {
		var c Customer
		if err := rows.Scan(&c.ID, &c.Name, &c.Phone, &c.Address, &c.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, c)
	}
	return list, nil
}

func GetCustomerByID(id int) (*Customer, error) {
	var c Customer
	err := DB.QueryRow("SELECT id, name, phone, address, created_at FROM customers WHERE id = ?", id).
		Scan(&c.ID, &c.Name, &c.Phone, &c.Address, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func AddCustomer(name, phone, address string) (int64, error) {
	result, err := DB.Exec("INSERT INTO customers (name, phone, address) VALUES (?, ?, ?)", name, phone, address)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func UpdateCustomer(id int, name, phone, address string) error {
	_, err := DB.Exec("UPDATE customers SET name = ?, phone = ?, address = ? WHERE id = ?", name, phone, address, id)
	return err
}

func DeleteCustomer(id int) error {
	_, err := DB.Exec("DELETE FROM customers WHERE id = ?", id)
	return err
}
