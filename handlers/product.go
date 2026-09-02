package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"order-system/models"
	"strconv"
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
		Name  string  `json:"name"`
		Spec  string  `json:"spec"`
		Unit  string  `json:"unit"`
		Stock int     `json:"stock"`
		Price float64 `json:"price"`
		BOM   []struct {
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

	id, err := models.AddProduct(req.Name, req.Spec, req.Unit, req.Stock, req.Price)
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
		ID    int     `json:"id"`
		Name  string  `json:"name"`
		Spec  string  `json:"spec"`
		Unit  string  `json:"unit"`
		Stock int     `json:"stock"`
		Price float64 `json:"price"`
		BOM   []struct {
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
	if req.Stock < 0 {
		http.Error(w, "Stock cannot be negative", http.StatusBadRequest)
		return
	}
	if req.Price < 0 {
		http.Error(w, "Price cannot be negative", http.StatusBadRequest)
		return
	}

	err := models.UpdateProduct(req.ID, req.Name, req.Spec, req.Unit, req.Stock, req.Price)
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

// ProduceProduct 生产入库（增加成品库存，根据BOM扣减原材料）
func ProduceProduct(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ProductID int `json:"product_id"`
		Quantity  int `json:"quantity"`
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

	// 开启事务
	tx, err := models.DB.Begin()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// 检查产品是否存在
	var exists bool
	err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM products WHERE id = ?)", req.ProductID).Scan(&exists)
	if err != nil || !exists {
		http.Error(w, "Product not found", http.StatusNotFound)
		return
	}

	// 1. 增加成品库存
	_, err = tx.Exec("UPDATE products SET stock = stock + ? WHERE id = ?", req.Quantity, req.ProductID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 2. 获取产品BOM
	boms, err := models.GetBOMByProduct(req.ProductID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 如果产品没有BOM，可以继续（不扣原材料），但建议给出提示
	if len(boms) == 0 {
		// 无BOM，提交事务后返回警告（但不会报错）
		if err := tx.Commit(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message":    "成品库存已增加，但该产品无BOM，未扣减原材料",
			"product_id": req.ProductID,
			"quantity":   req.Quantity,
		})
		return
	}

	// 3. 扣减原材料
	for _, bom := range boms {
		deduct := bom.Quantity * float64(req.Quantity)
		_, err = tx.Exec("UPDATE raw_materials SET stock = stock - ? WHERE id = ?", deduct, bom.RawMaterialID)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to deduct raw material %d: %v", bom.RawMaterialID, err), http.StatusInternalServerError)
			return
		}
	}

	// 提交事务
	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 返回成功
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":    "生产入库成功",
		"product_id": req.ProductID,
		"quantity":   req.Quantity,
	})
}
