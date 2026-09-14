package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"order-system/models"
)

// 获取所有原材料列表
func ListRawMaterials(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	materials, err := models.GetAllRawMaterials()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(materials)
}

// 获取单个原材料信息
func GetRawMaterial(w http.ResponseWriter, r *http.Request) {
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

	material, err := models.GetRawMaterialByID(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(material)
}

// 添加原材料（支持小数库存，允许负库存）
func AddRawMaterial(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Name     string   `json:"name"`
		Spec     string   `json:"spec"`
		Stock    float64  `json:"stock"` // 改为 float64
		Unit     string   `json:"unit"`
		MinStock float64  `json:"min_stock"` // 改为 float64
		Price    float64  `json:"price"`
		Images   []string `json:"images"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}
	// 移除 req.Stock < 0 的校验，允许负库存
	if req.MinStock < 0 {
		req.MinStock = 0
	}
	if req.Price < 0 {
		req.Price = 0
	}

	id, err := models.AddRawMaterialWithImages(req.Name, req.Spec, req.Unit, req.Stock, req.MinStock, req.Price, req.Images)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":      id,
		"message": "原材料添加成功",
	})
}

// 更新原材料（支持小数库存，允许负库存）
func UpdateRawMaterial(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ID       int      `json:"id"`
		Name     string   `json:"name"`
		Spec     string   `json:"spec"`
		Stock    float64  `json:"stock"` // 改为 float64
		Unit     string   `json:"unit"`
		MinStock float64  `json:"min_stock"` // 改为 float64
		Price    float64  `json:"price"`
		Images   []string `json:"images"`
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
	// 移除 req.Stock < 0 的校验，允许负库存
	if req.MinStock < 0 {
		req.MinStock = 0
	}
	if req.Price < 0 {
		req.Price = 0
	}

	err := models.UpdateRawMaterialWithImages(req.ID, req.Name, req.Spec, req.Unit, req.Stock, req.MinStock, req.Price, req.Images)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "原材料更新成功",
	})
}

// 原材料入库操作（支持小数数量）
func RawMaterialInbound(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ID       int     `json:"id"`
		Quantity float64 `json:"quantity"` // 改为 float64
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.ID <= 0 {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}
	if req.Quantity <= 0 {
		http.Error(w, "Quantity must be positive", http.StatusBadRequest)
		return
	}

	err := models.UpdateRawMaterialStock(req.ID, req.Quantity)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	material, err := models.GetRawMaterialByID(req.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":   "入库成功",
		"new_stock": material.Stock,
		"material":  material,
	})
}

// 原材料出库操作（允许库存为负数，不检查库存是否充足）
func RawMaterialOutbound(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ID       int     `json:"id"`
		Quantity float64 `json:"quantity"` // 改为 float64
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.ID <= 0 {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}
	if req.Quantity <= 0 {
		http.Error(w, "Quantity must be positive", http.StatusBadRequest)
		return
	}

	// 直接扣减，不检查库存是否充足（允许负数）
	err := models.UpdateRawMaterialStock(req.ID, -req.Quantity)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	updatedMaterial, err := models.GetRawMaterialByID(req.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":   "出库成功",
		"new_stock": updatedMaterial.Stock,
		"material":  updatedMaterial,
	})
}

// deleteRawMaterialImageFiles 删除原材料关联的图鉴图片文件。
func deleteRawMaterialImageFiles(urls []string) {
	for _, url := range urls {
		rel := strings.TrimPrefix(url, "/")
		if !strings.HasPrefix(rel, "uploads/raw_material_images/") {
			log.Printf("跳过删除非原材料图鉴目录文件: %s", url)
			continue
		}
		localPath := filepath.FromSlash(rel)
		if err := os.Remove(localPath); err != nil && !os.IsNotExist(err) {
			log.Printf("删除原材料图鉴文件失败 %s: %v", localPath, err)
		}
	}
}

// 删除原材料
func DeleteRawMaterial(w http.ResponseWriter, r *http.Request) {
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

	material, err := models.GetRawMaterialByID(id)
	if err != nil {
		http.Error(w, "Raw material not found", http.StatusNotFound)
		return
	}

	err = models.DeleteRawMaterial(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	deleteRawMaterialImageFiles(material.Images)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "原材料删除成功",
	})
}

// 获取低库存预警列表
func GetLowStockRawMaterials(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	materials, err := models.GetLowStockMaterials()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(materials)
}

// UploadRawMaterialImages 上传原材料图鉴图片，支持一次选择多张。
func UploadRawMaterialImages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 30<<20)
	if err := r.ParseMultipartForm(30 << 20); err != nil {
		http.Error(w, "读取上传文件失败", http.StatusBadRequest)
		return
	}

	var fileHeaders []*multipart.FileHeader
	if r.MultipartForm != nil {
		fileHeaders = r.MultipartForm.File["files"]
		if len(fileHeaders) == 0 {
			fileHeaders = r.MultipartForm.File["file"]
		}
	}
	if len(fileHeaders) == 0 {
		http.Error(w, "请选择要上传的图片", http.StatusBadRequest)
		return
	}

	allowed := map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true}
	dir := filepath.Join("uploads", "raw_material_images")
	if err := os.MkdirAll(dir, 0755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var urls []string
	for _, header := range fileHeaders {
		ext := strings.ToLower(filepath.Ext(header.Filename))
		if !allowed[ext] {
			http.Error(w, "仅支持 jpg、jpeg、png、gif、webp 图片", http.StatusBadRequest)
			return
		}
		file, err := header.Open()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		filename := fmt.Sprintf("%d_%d%s", time.Now().UnixNano(), len(urls), ext)
		dst := filepath.Join(dir, filename)
		out, err := os.Create(dst)
		if err != nil {
			file.Close()
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if _, err := io.Copy(out, file); err != nil {
			out.Close()
			file.Close()
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out.Close()
		file.Close()
		urls = append(urls, "/"+filepath.ToSlash(dst))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"urls":    urls,
		"message": "上传成功",
	})
}
