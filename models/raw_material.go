package models

import (
	"encoding/json"
	"time"
)

type RawMaterial struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	Spec      string    `json:"spec"`
	Stock     float64   `json:"stock"`
	Unit      string    `json:"unit"`
	MinStock  float64   `json:"min_stock"`
	Price     float64   `json:"price"`
	Images    []string  `json:"images"`
	CreatedAt time.Time `json:"created_at"`
}

func encodeRawMaterialImages(images []string) (string, error) {
	if images == nil {
		images = []string{}
	}
	data, err := json.Marshal(images)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func parseRawMaterialImages(raw string) []string {
	if raw == "" {
		return []string{}
	}
	var images []string
	if err := json.Unmarshal([]byte(raw), &images); err != nil {
		return []string{}
	}
	if images == nil {
		return []string{}
	}
	return images
}

// 获取所有原材料
func GetAllRawMaterials() ([]RawMaterial, error) {
	rows, err := DB.Query(`
        SELECT id, name, spec, stock, unit, min_stock, price, COALESCE(images, ''), created_at
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
		var imagesRaw string
		err := rows.Scan(&m.ID, &m.Name, &m.Spec, &m.Stock, &m.Unit, &m.MinStock, &m.Price, &imagesRaw, &m.CreatedAt)
		if err != nil {
			return nil, err
		}
		m.Images = parseRawMaterialImages(imagesRaw)
		materials = append(materials, m)
	}
	return materials, nil
}

// 根据ID获取原材料
func GetRawMaterialByID(id int) (*RawMaterial, error) {
	var m RawMaterial
	var imagesRaw string
	err := DB.QueryRow(`
        SELECT id, name, spec, stock, unit, min_stock, price, COALESCE(images, ''), created_at
        FROM raw_materials
        WHERE id = ?
    `, id).Scan(&m.ID, &m.Name, &m.Spec, &m.Stock, &m.Unit, &m.MinStock, &m.Price, &imagesRaw, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	m.Images = parseRawMaterialImages(imagesRaw)
	return &m, nil
}

// AddRawMaterial 兼容原有调用，默认不包含图鉴图片。
func AddRawMaterial(name, spec, unit string, stock float64, minStock float64, price float64) (int64, error) {
	return AddRawMaterialWithImages(name, spec, unit, stock, minStock, price, nil)
}

// 添加原材料
func AddRawMaterialWithImages(name, spec, unit string, stock float64, minStock float64, price float64, images []string) (int64, error) {
	imagesJSON, err := encodeRawMaterialImages(images)
	if err != nil {
		return 0, err
	}
	result, err := DB.Exec(`
        INSERT INTO raw_materials (name, spec, stock, unit, min_stock, price, images)
        VALUES (?, ?, ?, ?, ?, ?, ?)
    `, name, spec, stock, unit, minStock, price, imagesJSON)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// UpdateRawMaterial 兼容原有调用，不包含图鉴参数。
func UpdateRawMaterial(id int, name, spec, unit string, stock float64, minStock float64, price float64) error {
	return UpdateRawMaterialWithImages(id, name, spec, unit, stock, minStock, price, nil)
}

// 更新原材料
func UpdateRawMaterialWithImages(id int, name, spec, unit string, stock float64, minStock float64, price float64, images []string) error {
	imagesJSON, err := encodeRawMaterialImages(images)
	if err != nil {
		return err
	}
	_, err = DB.Exec(`
        UPDATE raw_materials
        SET name = ?, spec = ?, stock = ?, unit = ?, min_stock = ?, price = ?, images = ?
        WHERE id = ?
    `, name, spec, stock, unit, minStock, price, imagesJSON, id)
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
        SELECT id, name, spec, stock, unit, min_stock, price, COALESCE(images, ''), created_at
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
		var imagesRaw string
		err := rows.Scan(&m.ID, &m.Name, &m.Spec, &m.Stock, &m.Unit, &m.MinStock, &m.Price, &imagesRaw, &m.CreatedAt)
		if err != nil {
			return nil, err
		}
		m.Images = parseRawMaterialImages(imagesRaw)
		materials = append(materials, m)
	}
	return materials, nil
}
