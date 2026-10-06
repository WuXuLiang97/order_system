package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"order-system/models"
	"strconv"
	"strings"
	"time"
)

// 获取所有产品列表（含BOM标记）
func ListProducts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	products, err := models.GetAllProducts()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 为每个产品添加 has_bom 字段
	type ProductWithBOM struct {
		models.Product
		HasBOM bool `json:"has_bom"`
	}
	result := make([]ProductWithBOM, len(products))

	for i, p := range products {
		boms, err := models.GetBOMByProduct(p.ID)
		if err != nil {
			// 出错时视为无BOM，不影响整体返回
			result[i] = ProductWithBOM{Product: p, HasBOM: false}
			continue
		}
		result[i] = ProductWithBOM{Product: p, HasBOM: len(boms) > 0}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// 获取单个产品信息（含BOM）
func GetProduct(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		http.Error(w, "Missing id parameter", http.StatusBadRequest)
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid id", http.StatusBadRequest)
		return
	}

	product, err := models.GetProductByID(id)
	if err != nil {
		http.Error(w, "Product not found", http.StatusNotFound)
		return
	}

	boms, err := models.GetBOMByProduct(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	result := struct {
		models.Product
		BOM []models.ProductBOM `json:"bom"`
	}{
		Product: *product,
		BOM:     boms,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// 添加产品（含BOM）
func AddProduct(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Name      string  `json:"name"`
		Spec      string  `json:"spec"`
		Unit      string  `json:"unit"`
		Packaging string  `json:"packaging"`
		Stock     int     `json:"stock"`
		Price     float64 `json:"price"`
		BOM       []struct {
			RawMaterialID int     `json:"raw_material_id"`
			Quantity      float64 `json:"quantity"`
		} `json:"bom"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}
	if req.Stock < 0 {
		req.Stock = 0
	}
	if req.Price < 0 {
		req.Price = 0
	}

	id, err := models.AddProduct(req.Name, req.Spec, req.Unit, req.Packaging, req.Stock, req.Price)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 保存BOM（如果有）
	if len(req.BOM) > 0 {
		bomItems := make([]models.BOMItem, len(req.BOM))
		for i, b := range req.BOM {
			bomItems[i].RawMaterialID = b.RawMaterialID
			bomItems[i].Quantity = b.Quantity
		}
		if err := models.ReplaceBOM(int(id), bomItems); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":      id,
		"message": "Product added successfully",
	})
}

// 更新产品信息（含BOM）
func UpdateProduct(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ID        int     `json:"id"`
		Name      string  `json:"name"`
		Spec      string  `json:"spec"`
		Unit      string  `json:"unit"`
		Packaging string  `json:"packaging"`
		Price     float64 `json:"price"`
		BOM       []struct {
			RawMaterialID int     `json:"raw_material_id"`
			Quantity      float64 `json:"quantity"`
		} `json:"bom"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.ID <= 0 {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}
	if req.Price < 0 {
		http.Error(w, "Price cannot be negative", http.StatusBadRequest)
		return
	}

	err := models.UpdateProduct(req.ID, req.Name, req.Spec, req.Unit, req.Packaging, 0, req.Price)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 更新BOM
	if len(req.BOM) > 0 {
		bomItems := make([]models.BOMItem, len(req.BOM))
		for i, b := range req.BOM {
			bomItems[i].RawMaterialID = b.RawMaterialID
			bomItems[i].Quantity = b.Quantity
		}
		if err := models.ReplaceBOM(req.ID, bomItems); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		// 清空BOM
		if err := models.ReplaceBOM(req.ID, nil); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Product updated successfully",
	})
}

// 删除产品（级联删除BOM）
func DeleteProduct(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		http.Error(w, "Missing id parameter", http.StatusBadRequest)
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid id", http.StatusBadRequest)
		return
	}

	err = models.DeleteProduct(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Product deleted successfully",
	})
}

// productionItemRequest 描述一行生产入库明细。
type productionItemRequest struct {
	ProductID int     `json:"product_id"`
	Quantity  float64 `json:"quantity"`
}

// productionLineResult 返回单个产品的入库结果，用于批量汇总与前端展示。
type productionLineResult struct {
	ProductID    int     `json:"product_id"`
	ProductName  string  `json:"product_name"`
	Quantity     float64 `json:"quantity"`
	MaterialCost float64 `json:"material_cost"`
	UnitCost     float64 `json:"unit_cost"`
	HasBOM       bool    `json:"has_bom"`
}

type productionBOMLine struct {
	RawMaterialID int
	Quantity      float64
}

// normalizeProductionItems 校验并合并生产入库明细：同一产品合并数量，避免重复领料、重复计成本。
func normalizeProductionItems(items []productionItemRequest) ([]productionItemRequest, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("请至少填写一个入库产品")
	}
	merged := make([]productionItemRequest, 0, len(items))
	indexByProduct := make(map[int]int)
	for _, item := range items {
		if item.ProductID <= 0 {
			return nil, fmt.Errorf("产品ID不正确")
		}
		if item.Quantity <= 0 {
			return nil, fmt.Errorf("生产数量必须大于0")
		}
		if idx, ok := indexByProduct[item.ProductID]; ok {
			merged[idx].Quantity += item.Quantity
			continue
		}
		indexByProduct[item.ProductID] = len(merged)
		merged = append(merged, item)
	}
	return merged, nil
}

// produceOneProductTx 在调用方事务内完成单个产品的生产入库：
// 先按 BOM 扣减原材料并冻结领料成本，再把成本结转到成品库存。
func produceOneProductTx(tx *sql.Tx, item productionItemRequest, productionNo, batchNo, productionRemark string, occurredAt time.Time, userID int) (productionLineResult, error) {
	var result productionLineResult
	result.ProductID = item.ProductID
	result.Quantity = item.Quantity

	var productName string
	if err := tx.QueryRow("SELECT name FROM products WHERE id = ? FOR UPDATE", item.ProductID).Scan(&productName); err != nil {
		if err == sql.ErrNoRows {
			return result, fmt.Errorf("产品不存在或已被删除")
		}
		return result, err
	}
	result.ProductName = productName

	rows, err := tx.Query("SELECT raw_material_id, quantity FROM product_bom WHERE product_id = ? ORDER BY raw_material_id ASC", item.ProductID)
	if err != nil {
		return result, err
	}
	var boms []productionBOMLine
	for rows.Next() {
		var line productionBOMLine
		if err := rows.Scan(&line.RawMaterialID, &line.Quantity); err != nil {
			rows.Close()
			return result, err
		}
		boms = append(boms, line)
	}
	if err := rows.Close(); err != nil {
		return result, err
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	result.HasBOM = len(boms) > 0

	materialCost := 0.0
	for _, bom := range boms {
		consumeQty := bom.Quantity * item.Quantity
		movement, err := models.ApplyStockDeltaTx(tx, models.StockMovementInput{
			ItemType:        models.InventoryItemRawMaterial,
			ItemID:          bom.RawMaterialID,
			Quantity:        -consumeQty,
			MovementType:    models.MovementProductionConsume,
			ReferenceType:   "production",
			ReferenceNo:     productionNo,
			OccurredAt:      occurredAt,
			BatchNo:         batchNo,
			Remark:          productionRemark,
			CreatedByUserID: userID,
		})
		if err != nil {
			return result, fmt.Errorf("产品「%s」领料失败：%v", productName, err)
		}
		materialCost += -movement.TotalCost
	}

	unitCost := 0.0
	if item.Quantity > 0 {
		unitCost = materialCost / item.Quantity
	}
	if _, err := models.ApplyStockDeltaTx(tx, models.StockMovementInput{
		ItemType:        models.InventoryItemProduct,
		ItemID:          item.ProductID,
		Quantity:        item.Quantity,
		UnitCost:        unitCost,
		MovementType:    models.MovementProductionIn,
		ReferenceType:   "production",
		ReferenceNo:     productionNo,
		OccurredAt:      occurredAt,
		BatchNo:         batchNo,
		Remark:          productionRemark,
		CreatedByUserID: userID,
	}); err != nil {
		return result, err
	}

	result.MaterialCost = materialCost
	result.UnitCost = unitCost
	return result, nil
}

// ProduceProduct 生产入库：一次可提交多个产品，按实际领料成本扣原材料，
// 并把冻结的材料成本分别转入对应成品库存。所有明细在同一事务内完成，任一失败整单回滚。
func ProduceProduct(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ProductID    int                     `json:"product_id"`
		Quantity     float64                 `json:"quantity"`
		Items        []productionItemRequest `json:"items"`
		BusinessDate string                  `json:"business_date"`
		BatchNo      string                  `json:"batch_no"`
		Remark       string                  `json:"remark"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// 兼容旧的单产品提交：没有 items 时用 product_id + quantity 组装一行。
	items := req.Items
	if len(items) == 0 && req.ProductID > 0 && req.Quantity > 0 {
		items = []productionItemRequest{{ProductID: req.ProductID, Quantity: req.Quantity}}
	}
	if len(items) == 0 {
		http.Error(w, "请至少填写一个入库产品", http.StatusBadRequest)
		return
	}

	merged, err := normalizeProductionItems(items)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	occurredAt, err := businessTimeFromDate(req.BusinessDate)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	productionNo := "SC" + time.Now().Format("20060102150405")
	batchNo := strings.TrimSpace(req.BatchNo)
	if batchNo == "" {
		batchNo = productionNo
	}
	remark := strings.TrimSpace(req.Remark)
	userID := 0
	if user := CurrentUser(r); user != nil {
		userID = user.ID
	}
	productionRemark := "生产完工入库"
	if remark != "" {
		productionRemark += "：" + remark
	}

	tx, err := models.DB.Begin()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	results := make([]productionLineResult, 0, len(merged))
	totalQuantity := 0.0
	totalMaterialCost := 0.0
	noBOMCount := 0
	for _, item := range merged {
		result, err := produceOneProductTx(tx, item, productionNo, batchNo, productionRemark, occurredAt, userID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if !result.HasBOM {
			noBOMCount++
		}
		results = append(results, result)
		totalQuantity += result.Quantity
		totalMaterialCost += result.MaterialCost
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	message := fmt.Sprintf("生产入库成功，共 %d 种产品，材料成本已冻结", len(results))
	if noBOMCount > 0 {
		message += fmt.Sprintf("（其中 %d 种无BOM，本次成本记为0）", noBOMCount)
	}

	payload := map[string]interface{}{
		"message":             message,
		"production_no":       productionNo,
		"batch_no":            batchNo,
		"items":               results,
		"total_quantity":      totalQuantity,
		"total_material_cost": totalMaterialCost,
	}
	// 兼容旧的单产品返回字段。
	if len(results) == 1 {
		payload["product_id"] = results[0].ProductID
		payload["quantity"] = results[0].Quantity
		payload["material_cost"] = results[0].MaterialCost
		payload["unit_cost"] = results[0].UnitCost
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(payload)
}
