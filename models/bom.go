package models

import "time"

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
