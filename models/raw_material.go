package models

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type RawMaterial struct {
	ID             int       `json:"id"`
	Name           string    `json:"name"`
	Spec           string    `json:"spec"`
	Stock          float64   `json:"stock"`
	ReservedStock  float64   `json:"reserved_stock"`
	AvailableStock float64   `json:"available_stock"`
	Unit           string    `json:"unit"`
	MinStock       float64   `json:"min_stock"`
	Price          float64   `json:"price"`
	AvgCost        float64   `json:"avg_cost"`
	Images         []string  `json:"images"`
	CreatedAt      time.Time `json:"created_at"`
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

const rawMaterialSelect = `
	SELECT rm.id, rm.name, rm.spec, rm.stock,
	       COALESCE(r.reserved, 0) AS reserved_stock,
	       rm.stock - COALESCE(r.reserved, 0) AS available_stock,
	       rm.unit, rm.min_stock, rm.price, rm.avg_cost,
	       COALESCE(rm.images, ''), rm.created_at
	FROM raw_materials rm
	LEFT JOIN (
		SELECT item_id, SUM(quantity - consumed_quantity - released_quantity) AS reserved
		FROM inventory_reservations
		WHERE item_type = 'raw_material' AND status = 'active'
		GROUP BY item_id
	) r ON r.item_id = rm.id`

func scanRawMaterial(scanner interface {
	Scan(dest ...interface{}) error
}) (*RawMaterial, error) {
	var m RawMaterial
	var imagesRaw string
	err := scanner.Scan(&m.ID, &m.Name, &m.Spec, &m.Stock, &m.ReservedStock, &m.AvailableStock,
		&m.Unit, &m.MinStock, &m.Price, &m.AvgCost, &imagesRaw, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	m.Images = parseRawMaterialImages(imagesRaw)
	return &m, nil
}

// 获取所有原材料
func GetAllRawMaterials() ([]RawMaterial, error) {
	rows, err := DB.Query(rawMaterialSelect + " ORDER BY rm.id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var materials []RawMaterial
	for rows.Next() {
		m, err := scanRawMaterial(rows)
		if err != nil {
			return nil, err
		}
		materials = append(materials, *m)
	}
	return materials, rows.Err()
}

// 根据ID获取原材料
func GetRawMaterialByID(id int) (*RawMaterial, error) {
	return scanRawMaterial(DB.QueryRow(rawMaterialSelect+" WHERE rm.id = ?", id))
}

// GetRawMaterialIdentityTx 在事务内读取原材料当前的名称、规格和单位，用于采购明细按ID同步。
func GetRawMaterialIdentityTx(tx *sql.Tx, id int) (string, string, string, error) {
	var name, spec, unit string
	err := tx.QueryRow(`
		SELECT name, COALESCE(spec, ''), COALESCE(unit, '')
		FROM raw_materials
		WHERE id = ?
	`, id).Scan(&name, &spec, &unit)
	return name, spec, unit, err
}

// AddRawMaterial 兼容原有调用，默认不包含图鉴图片。
func AddRawMaterial(name, spec, unit string, stock float64, minStock float64, price float64) (int64, error) {
	return AddRawMaterialWithImages(name, spec, unit, stock, minStock, price, nil)
}

// 添加原材料。初始库存写入期初流水。
func AddRawMaterialWithImages(name, spec, unit string, stock float64, minStock float64, price float64, images []string) (int64, error) {
	imagesJSON, err := encodeRawMaterialImages(images)
	if err != nil {
		return 0, err
	}
	tx, err := DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	result, err := tx.Exec(`
		INSERT INTO raw_materials (name, spec, stock, unit, min_stock, price, avg_cost, images)
		VALUES (?, ?, 0, ?, ?, ?, 0, ?)
	`, name, spec, unit, minStock, price, imagesJSON)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	if stock > 0 {
		if _, err := ApplyStockDeltaTx(tx, StockMovementInput{
			ItemType:      InventoryItemRawMaterial,
			ItemID:        int(id),
			Quantity:      stock,
			UnitCost:      price,
			MovementType:  MovementOpening,
			ReferenceType: "raw_material_create",
			ReferenceID:   id,
			Remark:        "新建原材料期初库存",
		}); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

// UpdateRawMaterial 兼容原有调用，不包含图鉴参数。
func UpdateRawMaterial(id int, name, spec, unit string, stock float64, minStock float64, price float64) error {
	return UpdateRawMaterialWithImages(id, name, spec, unit, stock, minStock, price, nil)
}

// 更新原材料资料。库存不属于资料字段，必须通过入库、出库、生产领料或盘点改变。
func UpdateRawMaterialWithImages(id int, name, spec, unit string, _ float64, minStock float64, price float64, images []string) error {
	imagesJSON, err := encodeRawMaterialImages(images)
	if err != nil {
		return err
	}
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		UPDATE raw_materials
		SET name = ?, spec = ?, unit = ?, min_stock = ?, price = ?, images = ?
		WHERE id = ?
	`, name, spec, unit, minStock, price, imagesJSON, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		UPDATE purchase_materials
		SET material_name = ?, spec = ?, unit = ?
		WHERE raw_material_id = ?
	`, name, spec, unit, id); err != nil {
		return err
	}
	return tx.Commit()
}

type RawMaterialMovementInput struct {
	ID              int
	Quantity        float64
	UnitCost        *float64
	OccurredAt      time.Time
	BatchNo         string
	Remark          string
	CreatedByUserID int
}

// RecordRawMaterialMovement 记录带业务日期、来源、成本和经办人的手工出入库。
func RecordRawMaterialMovement(input RawMaterialMovementInput) (StockMovementResult, error) {
	var result StockMovementResult
	if input.ID <= 0 {
		return result, fmt.Errorf("原材料ID无效")
	}
	if input.Quantity == 0 {
		return result, fmt.Errorf("库存变动数量不能为0")
	}
	var price, avgCost float64
	if err := DB.QueryRow("SELECT price, avg_cost FROM raw_materials WHERE id = ?", input.ID).Scan(&price, &avgCost); err != nil {
		return result, err
	}
	if price <= 0 {
		price = avgCost
	}
	movingOut := input.Quantity < 0
	unitCost := price
	overrideUnitCost := false
	if input.UnitCost != nil {
		unitCost = *input.UnitCost
		overrideUnitCost = true
	}
	if movingOut {
		unitCost = avgCost
		overrideUnitCost = false
	}
	movementType := MovementManualIn
	if movingOut {
		movementType = MovementManualOut
	}
	remark := strings.TrimSpace(input.Remark)
	if remark == "" {
		if movingOut {
			remark = "原材料手工出库"
		} else {
			remark = "原材料手工入库"
		}
	}
	tx, err := DB.Begin()
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	result, err = ApplyStockDeltaTx(tx, StockMovementInput{
		ItemType:         InventoryItemRawMaterial,
		ItemID:           input.ID,
		Quantity:         input.Quantity,
		UnitCost:         unitCost,
		MovementType:     movementType,
		ReferenceType:    "manual",
		ReferenceID:      int64(input.ID),
		OccurredAt:       input.OccurredAt,
		BatchNo:          strings.TrimSpace(input.BatchNo),
		Remark:           remark,
		OverrideUnitCost: overrideUnitCost,
		CreatedByUserID:  input.CreatedByUserID,
	})
	if err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}

// UpdateRawMaterialStock 保留旧调用入口，默认由系统时间与当前用户上下文之外调用。
func UpdateRawMaterialStock(id int, quantity float64) error {
	_, err := RecordRawMaterialMovement(RawMaterialMovementInput{
		ID: id, Quantity: quantity, OccurredAt: time.Now(),
	})
	return err
}

// 删除原材料
func DeleteRawMaterial(id int) error {
	_, err := DB.Exec("DELETE FROM raw_materials WHERE id = ?", id)
	return err
}

// 获取低库存原材料（按可用库存预警）。
func GetLowStockMaterials() ([]RawMaterial, error) {
	rows, err := DB.Query(rawMaterialSelect + `
		WHERE rm.stock - COALESCE(r.reserved, 0) <= rm.min_stock
		ORDER BY rm.stock - COALESCE(r.reserved, 0) ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var materials []RawMaterial
	for rows.Next() {
		m, err := scanRawMaterial(rows)
		if err != nil {
			return nil, err
		}
		materials = append(materials, *m)
	}
	return materials, rows.Err()
}
