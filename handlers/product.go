package handlers

import (
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

// ProduceProduct 生产入库：按实际领料成本扣原材料，并把冻结的材料成本转入成品。
func ProduceProduct(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ProductID    int    `json:"product_id"`
		Quantity     int    `json:"quantity"`
		BusinessDate string `json:"business_date"`
		BatchNo      string `json:"batch_no"`
		Remark       string `json:"remark"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.ProductID <= 0 {
		http.Error(w, "Invalid product ID", http.StatusBadRequest)
		return
	}
	if req.Quantity <= 0 {
		http.Error(w, "Quantity must be greater than 0", http.StatusBadRequest)
		return
	}
	occurredAt, err := businessTimeFromDate(req.BusinessDate)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	tx, err := models.DB.Begin()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	var productExists int
	if err := tx.QueryRow("SELECT 1 FROM products WHERE id = ? FOR UPDATE", req.ProductID).Scan(&productExists); err != nil {
		http.Error(w, "Product not found", http.StatusNotFound)
		return
	}

	type bomLine struct {
		RawMaterialID int
		Quantity      float64
	}
	rows, err := tx.Query("SELECT raw_material_id, quantity FROM product_bom WHERE product_id = ? ORDER BY raw_material_id ASC", req.ProductID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var boms []bomLine
	for rows.Next() {
		var line bomLine
		if err := rows.Scan(&line.RawMaterialID, &line.Quantity); err != nil {
			rows.Close()
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		boms = append(boms, line)
	}
	if err := rows.Close(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
	materialCost := 0.0
	for _, bom := range boms {
		consumeQty := bom.Quantity * float64(req.Quantity)
		result, err := models.ApplyStockDeltaTx(tx, models.StockMovementInput{
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
			http.Error(w, fmt.Sprintf("生产领料失败：%v", err), http.StatusBadRequest)
			return
		}
		materialCost += -result.TotalCost
	}

	unitCost := 0.0
	if req.Quantity > 0 {
		unitCost = materialCost / float64(req.Quantity)
	}
	if _, err := models.ApplyStockDeltaTx(tx, models.StockMovementInput{
		ItemType:        models.InventoryItemProduct,
		ItemID:          req.ProductID,
		Quantity:        float64(req.Quantity),
		UnitCost:        unitCost,
		MovementType:    models.MovementProductionIn,
		ReferenceType:   "production",
		ReferenceNo:     productionNo,
		OccurredAt:      occurredAt,
		BatchNo:         batchNo,
		Remark:          productionRemark,
		CreatedByUserID: userID,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	message := "生产入库成功，材料成本已冻结"
	if len(boms) == 0 {
		message = "成品库存已增加，但该产品无BOM，本次生产成本记为0"
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":       message,
		"product_id":    req.ProductID,
		"quantity":      req.Quantity,
		"production_no": productionNo,
		"batch_no":      batchNo,
		"material_cost": materialCost,
		"unit_cost":     unitCost,
	})
}
