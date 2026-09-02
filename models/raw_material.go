package models

import "time"

type RawMaterial struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	Spec      string    `json:"spec"`
	Stock     float64   `json:"stock"`
	Unit      string    `json:"unit"`
	MinStock  float64   `json:"min_stock"`
	Price     float64   `json:"price"`
	CreatedAt time.Time `json:"created_at"`
}

// 获取所有原材料
func GetAllRawMaterials() ([]RawMaterial, error) {
	rows, err := DB.Query(`
        SELECT id, name, spec, stock, unit, min_stock, price, created_at
        FROM raw_materials
        ORDER BY id DESC
    `)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var materials []RawMaterial
	for rows.Next() {
		var m RawMaterial
		err := rows.Scan(&m.ID, &m.Name, &m.Spec, &m.Stock, &m.Unit, &m.MinStock, &m.Price, &m.CreatedAt)
		if err != nil {
			return nil, err
		}
		materials = append(materials, m)
	}
	return materials, nil
}

// 根据ID获取原材料
func GetRawMaterialByID(id int) (*RawMaterial, error) {
	var m RawMaterial
	err := DB.QueryRow(`
        SELECT id, name, spec, stock, unit, min_stock, price, created_at
        FROM raw_materials
        WHERE id = ?
    `, id).Scan(&m.ID, &m.Name, &m.Spec, &m.Stock, &m.Unit, &m.MinStock, &m.Price, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// 添加原材料
func AddRawMaterial(name, spec, unit string, stock float64, minStock float64, price float64) (int64, error) {
	result, err := DB.Exec(`
        INSERT INTO raw_materials (name, spec, stock, unit, min_stock, price)
        VALUES (?, ?, ?, ?, ?, ?)
    `, name, spec, stock, unit, minStock, price)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// 更新原材料
func UpdateRawMaterial(id int, name, spec, unit string, stock float64, minStock float64, price float64) error {
	_, err := DB.Exec(`
        UPDATE raw_materials
        SET name = ?, spec = ?, stock = ?, unit = ?, min_stock = ?, price = ?
        WHERE id = ?
    `, name, spec, stock, unit, minStock, price, id)
	return err
}

// 更新原材料库存（入库/出库）
func UpdateRawMaterialStock(id int, quantity float64) error {
	_, err := DB.Exec("UPDATE raw_materials SET stock = stock + ? WHERE id = ?", quantity, id)
	return err
}

// 删除原材料
func DeleteRawMaterial(id int) error {
	_, err := DB.Exec("DELETE FROM raw_materials WHERE id = ?", id)
	return err
}

// 获取低库存原材料（预警）
func GetLowStockMaterials() ([]RawMaterial, error) {
	rows, err := DB.Query(`
        SELECT id, name, spec, stock, unit, min_stock, price, created_at
        FROM raw_materials
        WHERE stock <= min_stock
        ORDER BY stock ASC
    `)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var materials []RawMaterial
	for rows.Next() {
		var m RawMaterial
		err := rows.Scan(&m.ID, &m.Name, &m.Spec, &m.Stock, &m.Unit, &m.MinStock, &m.Price, &m.CreatedAt)
		if err != nil {
			return nil, err
		}
		materials = append(materials, m)
	}
	return materials, nil
}
