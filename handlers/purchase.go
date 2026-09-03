package handlers

import (
	"database/sql"
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

const (
	PurchaseMaterialTypeRawMaterial = "原材料"
	PurchaseMaterialTypeFixedAsset  = "固定资产"
	PurchaseMaterialTypeOffice      = "办公用品"
	PurchaseMaterialTypeEquipment   = "设备"
)

var purchaseMaterialTypes = map[string]bool{
	PurchaseMaterialTypeRawMaterial: true,
	PurchaseMaterialTypeFixedAsset:  true,
	PurchaseMaterialTypeOffice:      true,
	PurchaseMaterialTypeEquipment:   true,
}

type purchaseMaterialRequest struct {
	MaterialName        string   `json:"material_name"`
	MaterialType        string   `json:"material_type"`
	Spec                string   `json:"spec"`
	Unit                string   `json:"unit"`
	Quantity            float64  `json:"quantity"`
	Price               float64  `json:"price"`
	Amount              float64  `json:"amount"`
	Supplier            string   `json:"supplier"`
	Freight             float64  `json:"freight"`
	PurchaseDate        string   `json:"purchase_date"`
	ExpectedArrivalDate string   `json:"expected_arrival_date"`
	ActualArrivalDate   string   `json:"actual_arrival_date"`
	PaymentStatus       string   `json:"payment_status"`
	Status              int      `json:"status"`
	Remark              string   `json:"remark"`
	PaymentReceipts     []string `json:"payment_receipts"`
}

func parsePurchaseDate(s string) (interface{}, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func validatePurchaseRequest(req purchaseMaterialRequest) string {
	if strings.TrimSpace(req.MaterialName) == "" {
		return "物料名称不能为空"
	}
	if !purchaseMaterialTypes[req.MaterialType] {
		return "物料类型不正确"
	}
	if req.Quantity <= 0 {
		return "采购数量必须大于0"
	}
	if req.Price < 0 || req.Freight < 0 {
		return "单价和运费不能为负数"
	}
	if req.Status != 0 && req.Status != 1 {
		return "采购状态不正确"
	}
	if req.PaymentStatus != "" && req.PaymentStatus != "未付款" && req.PaymentStatus != "已付款" {
		return "付款状态不正确"
	}
	return ""
}

// addRawMaterialStockTx 根据物料名称和规格型号，将采购数量加入原材料库存。
func addRawMaterialStockTx(tx *sql.Tx, name, spec, unit string, quantity, price float64) error {
	var id int
	err := tx.QueryRow("SELECT id FROM raw_materials WHERE name = ? AND COALESCE(spec, '') = ? ORDER BY id ASC LIMIT 1", name, spec).Scan(&id)
	if err == sql.ErrNoRows {
		_, err = tx.Exec("INSERT INTO raw_materials (name, spec, stock, unit, min_stock, price) VALUES (?, ?, ?, ?, 0, ?)", name, spec, quantity, unit, price)
		return err
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec("UPDATE raw_materials SET stock = stock + ? WHERE id = ?", quantity, id)
	return err
}

// ListPurchaseMaterials 获取采购物料列表
func ListPurchaseMaterials(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	list, err := models.GetAllPurchaseMaterials()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// GetPurchaseMaterial 获取单个采购物料
func GetPurchaseMaterial(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || id <= 0 {
		http.Error(w, "Invalid id", http.StatusBadRequest)
		return
	}
	p, err := models.GetPurchaseMaterialByID(id)
	if err != nil {
		http.Error(w, "Purchase material not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(p)
}

// AddPurchaseMaterial 添加采购物料
func AddPurchaseMaterial(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req purchaseMaterialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if msg := validatePurchaseRequest(req); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	paymentStatus := strings.TrimSpace(req.PaymentStatus)
	if paymentStatus == "" {
		paymentStatus = "未付款"
	}

	purchaseDate, err := parsePurchaseDate(req.PurchaseDate)
	if err != nil {
		http.Error(w, "采购日期格式不正确", http.StatusBadRequest)
		return
	}
	expectedDate, err := parsePurchaseDate(req.ExpectedArrivalDate)
	if err != nil {
		http.Error(w, "预计到货日期格式不正确", http.StatusBadRequest)
		return
	}
	actualDate, err := parsePurchaseDate(req.ActualArrivalDate)
	if err != nil {
		http.Error(w, "实际到货日期格式不正确", http.StatusBadRequest)
		return
	}
	if req.Status == 1 && actualDate == nil {
		actualDate = time.Now()
	}

	amount := req.Quantity * req.Price
	paymentReceiptJSON, err := json.Marshal(req.PaymentReceipts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	stockAdded := 0
	if req.Status == 1 && req.MaterialType == PurchaseMaterialTypeRawMaterial {
		stockAdded = 1
	}

	tx, err := models.DB.Begin()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	result, err := tx.Exec(`
        INSERT INTO purchase_materials
        (material_name, material_type, spec, unit, quantity, price, amount, supplier, freight,
         purchase_date, expected_arrival_date, actual_arrival_date, payment_status, status, remark, payment_receipt, stock_added)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `, req.MaterialName, req.MaterialType, req.Spec, req.Unit, req.Quantity, req.Price, amount,
		req.Supplier, req.Freight, purchaseDate, expectedDate, actualDate, paymentStatus, req.Status, req.Remark, string(paymentReceiptJSON), stockAdded)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	id, _ := result.LastInsertId()

	if req.Status == 1 && req.MaterialType == PurchaseMaterialTypeRawMaterial {
		if err := addRawMaterialStockTx(tx, req.MaterialName, req.Spec, req.Unit, req.Quantity, req.Price); err != nil {
			http.Error(w, fmt.Sprintf("加入原材料库存失败: %v", err), http.StatusInternalServerError)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":      id,
		"message": "采购物料添加成功",
	})
}

// UpdatePurchaseMaterial 更新采购物料
func UpdatePurchaseMaterial(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID int `json:"id"`
		purchaseMaterialRequest
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.ID <= 0 {
		http.Error(w, "Invalid id", http.StatusBadRequest)
		return
	}
	if msg := validatePurchaseRequest(req.purchaseMaterialRequest); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	paymentStatus := strings.TrimSpace(req.PaymentStatus)
	if paymentStatus == "" {
		paymentStatus = "未付款"
	}

	purchaseDate, err := parsePurchaseDate(req.PurchaseDate)
	if err != nil {
		http.Error(w, "采购日期格式不正确", http.StatusBadRequest)
		return
	}
	expectedDate, err := parsePurchaseDate(req.ExpectedArrivalDate)
	if err != nil {
		http.Error(w, "预计到货日期格式不正确", http.StatusBadRequest)
		return
	}
	actualDate, err := parsePurchaseDate(req.ActualArrivalDate)
	if err != nil {
		http.Error(w, "实际到货日期格式不正确", http.StatusBadRequest)
		return
	}
	if req.Status == 1 && actualDate == nil {
		actualDate = time.Now()
	}

	amount := req.Quantity * req.Price
	paymentReceiptJSON, err := json.Marshal(req.PaymentReceipts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	tx, err := models.DB.Begin()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	var stockAdded int
	err = tx.QueryRow("SELECT stock_added FROM purchase_materials WHERE id = ? FOR UPDATE", req.ID).Scan(&stockAdded)
	if err == sql.ErrNoRows {
		http.Error(w, "Purchase material not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	newStockAdded := stockAdded
	if req.Status == 1 && req.MaterialType == PurchaseMaterialTypeRawMaterial && stockAdded == 0 {
		newStockAdded = 1
	}

	_, err = tx.Exec(`
        UPDATE purchase_materials SET
            material_name = ?, material_type = ?, spec = ?, unit = ?, quantity = ?, price = ?, amount = ?,
            supplier = ?, freight = ?, purchase_date = ?, expected_arrival_date = ?, actual_arrival_date = ?, payment_status = ?, status = ?,
            remark = ?, payment_receipt = ?, stock_added = ?
        WHERE id = ?
    `, req.MaterialName, req.MaterialType, req.Spec, req.Unit, req.Quantity, req.Price, amount,
		req.Supplier, req.Freight, purchaseDate, expectedDate, actualDate, paymentStatus, req.Status, req.Remark, string(paymentReceiptJSON),
		newStockAdded, req.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if req.Status == 1 && req.MaterialType == PurchaseMaterialTypeRawMaterial && stockAdded == 0 {
		if err := addRawMaterialStockTx(tx, req.MaterialName, req.Spec, req.Unit, req.Quantity, req.Price); err != nil {
			http.Error(w, fmt.Sprintf("加入原材料库存失败: %v", err), http.StatusInternalServerError)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "采购物料更新成功"})
}

// deleteReceiptFiles 删除采购物料关联的支付水单图片文件。
func deleteReceiptFiles(urls []string) {
	for _, url := range urls {
		rel := strings.TrimPrefix(url, "/")
		if !strings.HasPrefix(rel, "uploads/payment_receipts/") {
			log.Printf("跳过删除非支付水单目录文件: %s", url)
			continue
		}
		localPath := filepath.FromSlash(rel)
		if err := os.Remove(localPath); err != nil && !os.IsNotExist(err) {
			log.Printf("删除支付水单文件失败 %s: %v", localPath, err)
		}
	}
}

// DeletePurchaseMaterial 删除采购物料
func DeletePurchaseMaterial(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || id <= 0 {
		http.Error(w, "Invalid id", http.StatusBadRequest)
		return
	}
	p, err := models.GetPurchaseMaterialByID(id)
	if err != nil {
		http.Error(w, "Purchase material not found", http.StatusNotFound)
		return
	}
	if err := models.DeletePurchaseMaterial(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	deleteReceiptFiles(p.PaymentReceipts)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "采购物料删除成功"})
}

// GetPurchaseMaterialsSummary 获取指定月份的采购汇总金额（仅统计已到货）
func GetPurchaseMaterialsSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	month := strings.TrimSpace(r.URL.Query().Get("month"))
	if month == "" {
		month = time.Now().Format("2006-01")
	}
	if _, err := time.Parse("2006-01", month); err != nil {
		http.Error(w, "月份格式不正确", http.StatusBadRequest)
		return
	}
	year := month[:4]

	var monthlyTotal float64
	if err := models.DB.QueryRow("SELECT COALESCE(SUM(amount), 0) FROM purchase_materials WHERE status = 1 AND DATE_FORMAT(purchase_date, '%Y-%m') = ?", month).Scan(&monthlyTotal); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var yearlyTotal float64
	if err := models.DB.QueryRow("SELECT COALESCE(SUM(amount), 0) FROM purchase_materials WHERE status = 1 AND DATE_FORMAT(purchase_date, '%Y') = ?", year).Scan(&yearlyTotal); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var monthlyFreight float64
	if err := models.DB.QueryRow("SELECT COALESCE(SUM(freight), 0) FROM purchase_materials WHERE status = 1 AND DATE_FORMAT(purchase_date, '%Y-%m') = ?", month).Scan(&monthlyFreight); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var yearlyFreight float64
	if err := models.DB.QueryRow("SELECT COALESCE(SUM(freight), 0) FROM purchase_materials WHERE status = 1 AND DATE_FORMAT(purchase_date, '%Y') = ?", year).Scan(&yearlyFreight); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"month":           month,
		"year":            year,
		"monthly_total":   monthlyTotal,
		"yearly_total":    yearlyTotal,
		"monthly_freight": monthlyFreight,
		"yearly_freight":  yearlyFreight,
	})
}

// UploadPurchaseReceipt 上传支付水单图片
func UploadPurchaseReceipt(w http.ResponseWriter, r *http.Request) {
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
	dir := filepath.Join("uploads", "payment_receipts")
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
