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

type purchaseItemRequest struct {
	ID           int     `json:"id"`
	MaterialName string  `json:"material_name"`
	MaterialType string  `json:"material_type"`
	Spec         string  `json:"spec"`
	Unit         string  `json:"unit"`
	Quantity     float64 `json:"quantity"`
	Price        float64 `json:"price"`
}

type purchaseOrderRequest struct {
	ID                  int                   `json:"id"`
	Supplier            string                `json:"supplier"`
	Freight             float64               `json:"freight"`
	PurchaseDate        string                `json:"purchase_date"`
	ExpectedArrivalDate string                `json:"expected_arrival_date"`
	ActualArrivalDate   string                `json:"actual_arrival_date"`
	PaymentStatus       string                `json:"payment_status"`
	Status              int                   `json:"status"`
	Remark              string                `json:"remark"`
	PaymentReceipts     []string              `json:"payment_receipts"`
	Items               []purchaseItemRequest `json:"items"`
}

func parsePurchaseOrderDate(s string) (*time.Time, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", strings.TrimSpace(s))
	if err != nil {
		return nil, err
	}
	return &t, nil
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
func validatePurchaseOrderRequest(req purchaseOrderRequest) string {
	if req.Freight < 0 {
		return "运费不能为负数"
	}
	if req.Status != 0 && req.Status != 1 {
		return "采购状态不正确"
	}
	if req.PaymentStatus != "" && req.PaymentStatus != "未付款" && req.PaymentStatus != "已付款" {
		return "付款状态不正确"
	}
	if len(req.Items) == 0 {
		return "请至少添加一种采购物料"
	}
	for index, item := range req.Items {
		if strings.TrimSpace(item.MaterialName) == "" {
			return fmt.Sprintf("第%d行物料名称不能为空", index+1)
		}
		if !purchaseMaterialTypes[item.MaterialType] {
			return fmt.Sprintf("第%d行物料类型不正确", index+1)
		}
		if item.Quantity <= 0 {
			return fmt.Sprintf("第%d行采购数量必须大于0", index+1)
		}
		if item.Price < 0 {
			return fmt.Sprintf("第%d行单价不能为负数", index+1)
		}
	}
	return ""
}

func normalizePurchaseOrderRequest(req *purchaseOrderRequest) {
	req.Supplier = strings.TrimSpace(req.Supplier)
	req.PaymentStatus = strings.TrimSpace(req.PaymentStatus)
	if req.PaymentStatus == "" {
		req.PaymentStatus = "未付款"
	}
	req.Remark = strings.TrimSpace(req.Remark)
	for i := range req.Items {
		req.Items[i].MaterialName = strings.TrimSpace(req.Items[i].MaterialName)
		req.Items[i].MaterialType = strings.TrimSpace(req.Items[i].MaterialType)
		req.Items[i].Spec = strings.TrimSpace(req.Items[i].Spec)
		req.Items[i].Unit = strings.TrimSpace(req.Items[i].Unit)
		if req.Items[i].Unit == "" {
			req.Items[i].Unit = "个"
		}
	}
}

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

func insertPurchaseItemTx(tx *sql.Tx, orderID int, item purchaseItemRequest, req purchaseOrderRequest,
	purchaseDate, expectedDate, actualDate *time.Time, receiptJSON string, stockAdded int) error {
	amount := item.Quantity * item.Price
	_, err := tx.Exec(`
        INSERT INTO purchase_materials
            (purchase_order_id, material_name, material_type, spec, unit, quantity, price, amount,
             supplier, freight, purchase_date, expected_arrival_date, actual_arrival_date,
             payment_status, status, remark, payment_receipt, stock_added)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `, orderID, item.MaterialName, item.MaterialType, item.Spec, item.Unit, item.Quantity, item.Price, amount,
		req.Supplier, req.Freight, purchaseDate, expectedDate, actualDate, req.PaymentStatus, req.Status,
		req.Remark, receiptJSON, stockAdded)
	return err
}

func updatePurchaseItemTx(tx *sql.Tx, orderID int, item purchaseItemRequest, req purchaseOrderRequest,
	purchaseDate, expectedDate, actualDate *time.Time, receiptJSON string, stockAdded int) error {
	amount := item.Quantity * item.Price
	_, err := tx.Exec(`
        UPDATE purchase_materials SET
            material_name = ?, material_type = ?, spec = ?, unit = ?, quantity = ?, price = ?, amount = ?,
            supplier = ?, freight = ?, purchase_date = ?, expected_arrival_date = ?, actual_arrival_date = ?,
            payment_status = ?, status = ?, remark = ?, payment_receipt = ?, stock_added = ?
        WHERE id = ? AND purchase_order_id = ?
    `, item.MaterialName, item.MaterialType, item.Spec, item.Unit, item.Quantity, item.Price, amount,
		req.Supplier, req.Freight, purchaseDate, expectedDate, actualDate, req.PaymentStatus, req.Status,
		req.Remark, receiptJSON, stockAdded, item.ID, orderID)
	return err
}

// ListPurchaseMaterials 获取采购单列表（接口地址保持不变，返回结构已升级为采购单+明细）。
func ListPurchaseMaterials(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	list, err := models.GetAllPurchaseOrders()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

type purchaseRawMaterialOption struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Spec string `json:"spec"`
	Unit string `json:"unit"`
}

// ListPurchaseRawMaterialOptions 获取采购原材料可索引的名称、规格型号和单位。
func ListPurchaseRawMaterialOptions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	materials, err := models.GetAllRawMaterials()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	options := make([]purchaseRawMaterialOption, 0, len(materials))
	for _, material := range materials {
		options = append(options, purchaseRawMaterialOption{
			ID:   material.ID,
			Name: material.Name,
			Spec: material.Spec,
			Unit: material.Unit,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(options)
}

// GetPurchaseMaterial 获取单张采购单及其明细。
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
	order, err := models.GetPurchaseOrderByID(id)
	if err != nil {
		http.Error(w, "Purchase order not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(order)
}

// AddPurchaseMaterial 新增一张包含多种物料的采购单。
func AddPurchaseMaterial(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req purchaseOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	normalizePurchaseOrderRequest(&req)
	if msg := validatePurchaseOrderRequest(req); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}

	purchaseDate, err := parsePurchaseOrderDate(req.PurchaseDate)
	if err != nil {
		http.Error(w, "采购日期格式不正确", http.StatusBadRequest)
		return
	}
	expectedDate, err := parsePurchaseOrderDate(req.ExpectedArrivalDate)
	if err != nil {
		http.Error(w, "预计到货日期格式不正确", http.StatusBadRequest)
		return
	}
	actualDate, err := parsePurchaseOrderDate(req.ActualArrivalDate)
	if err != nil {
		http.Error(w, "实际到货日期格式不正确", http.StatusBadRequest)
		return
	}
	if req.Status == 1 && actualDate == nil {
		now := time.Now()
		actualDate = &now
	}
	receiptJSONBytes, err := json.Marshal(req.PaymentReceipts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	receiptJSON := string(receiptJSONBytes)

	tx, err := models.DB.Begin()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	placeholderNo := fmt.Sprintf("TMP-%d", time.Now().UnixNano())
	result, err := tx.Exec(`
        INSERT INTO purchase_orders
            (purchase_no, supplier, freight, purchase_date, expected_arrival_date,
             actual_arrival_date, payment_status, status, remark, payment_receipt)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `, placeholderNo, req.Supplier, req.Freight, purchaseDate, expectedDate, actualDate,
		req.PaymentStatus, req.Status, req.Remark, receiptJSON)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	orderID64, err := result.LastInsertId()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	orderID := int(orderID64)

	datePart := time.Now().Format("20060102")
	if purchaseDate != nil {
		datePart = purchaseDate.Format("20060102")
	}
	purchaseNo := fmt.Sprintf("CG%s-%05d", datePart, orderID)
	if _, err := tx.Exec("UPDATE purchase_orders SET purchase_no = ? WHERE id = ?", purchaseNo, orderID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	for _, item := range req.Items {
		stockAdded := 0
		needAddStock := req.Status == 1 && item.MaterialType == PurchaseMaterialTypeRawMaterial
		if needAddStock {
			stockAdded = 1
		}
		if err := insertPurchaseItemTx(tx, orderID, item, req, purchaseDate, expectedDate, actualDate, receiptJSON, stockAdded); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if needAddStock {
			if err := addRawMaterialStockTx(tx, item.MaterialName, item.Spec, item.Unit, item.Quantity, item.Price); err != nil {
				http.Error(w, fmt.Sprintf("加入原材料库存失败: %v", err), http.StatusInternalServerError)
				return
			}
		}
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":          orderID,
		"purchase_no": purchaseNo,
		"message":     "采购单添加成功",
	})
}

// UpdatePurchaseMaterial 更新采购单及全部明细；保留明细 ID 和原入库状态，避免重复增加库存。
func UpdatePurchaseMaterial(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req purchaseOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.ID <= 0 {
		http.Error(w, "Invalid id", http.StatusBadRequest)
		return
	}
	normalizePurchaseOrderRequest(&req)
	if msg := validatePurchaseOrderRequest(req); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}

	purchaseDate, err := parsePurchaseOrderDate(req.PurchaseDate)
	if err != nil {
		http.Error(w, "采购日期格式不正确", http.StatusBadRequest)
		return
	}
	expectedDate, err := parsePurchaseOrderDate(req.ExpectedArrivalDate)
	if err != nil {
		http.Error(w, "预计到货日期格式不正确", http.StatusBadRequest)
		return
	}
	actualDate, err := parsePurchaseOrderDate(req.ActualArrivalDate)
	if err != nil {
		http.Error(w, "实际到货日期格式不正确", http.StatusBadRequest)
		return
	}
	if req.Status == 1 && actualDate == nil {
		now := time.Now()
		actualDate = &now
	}
	receiptJSONBytes, err := json.Marshal(req.PaymentReceipts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	receiptJSON := string(receiptJSONBytes)

	tx, err := models.DB.Begin()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	var existingID int
	if err := tx.QueryRow("SELECT id FROM purchase_orders WHERE id = ? FOR UPDATE", req.ID).Scan(&existingID); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Purchase order not found", http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	existingStockAdded := make(map[int]int)
	rows, err := tx.Query("SELECT id, stock_added FROM purchase_materials WHERE purchase_order_id = ? FOR UPDATE", req.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for rows.Next() {
		var itemID, stockAdded int
		if err := rows.Scan(&itemID, &stockAdded); err != nil {
			rows.Close()
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		existingStockAdded[itemID] = stockAdded
	}
	if err := rows.Close(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	incomingIDs := make(map[int]bool)
	for _, item := range req.Items {
		if item.ID > 0 {
			if _, ok := existingStockAdded[item.ID]; !ok {
				http.Error(w, "采购明细不存在或不属于当前采购单", http.StatusBadRequest)
				return
			}
			incomingIDs[item.ID] = true
		}
	}
	for itemID := range existingStockAdded {
		if !incomingIDs[itemID] {
			if _, err := tx.Exec("DELETE FROM purchase_materials WHERE id = ? AND purchase_order_id = ?", itemID, req.ID); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}

	if _, err := tx.Exec(`
        UPDATE purchase_orders SET
            supplier = ?, freight = ?, purchase_date = ?, expected_arrival_date = ?, actual_arrival_date = ?,
            payment_status = ?, status = ?, remark = ?, payment_receipt = ?
        WHERE id = ?
    `, req.Supplier, req.Freight, purchaseDate, expectedDate, actualDate,
		req.PaymentStatus, req.Status, req.Remark, receiptJSON, req.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	for _, item := range req.Items {
		oldStockAdded := 0
		if item.ID > 0 {
			oldStockAdded = existingStockAdded[item.ID]
		}
		stockAdded := oldStockAdded
		needAddStock := req.Status == 1 && item.MaterialType == PurchaseMaterialTypeRawMaterial && oldStockAdded == 0
		if needAddStock {
			stockAdded = 1
		}

		if item.ID > 0 {
			if err := updatePurchaseItemTx(tx, req.ID, item, req, purchaseDate, expectedDate, actualDate, receiptJSON, stockAdded); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		} else {
			if err := insertPurchaseItemTx(tx, req.ID, item, req, purchaseDate, expectedDate, actualDate, receiptJSON, stockAdded); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		if needAddStock {
			if err := addRawMaterialStockTx(tx, item.MaterialName, item.Spec, item.Unit, item.Quantity, item.Price); err != nil {
				http.Error(w, fmt.Sprintf("加入原材料库存失败: %v", err), http.StatusInternalServerError)
				return
			}
		}
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "采购单更新成功"})
}

// deleteReceiptFiles 删除采购单关联的支付水单图片文件。
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

// DeletePurchaseMaterial 删除整张采购单及其全部明细。
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
	order, err := models.GetPurchaseOrderByID(id)
	if err != nil {
		http.Error(w, "Purchase order not found", http.StatusNotFound)
		return
	}
	if err := models.DeletePurchaseOrder(id); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Purchase order not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	deleteReceiptFiles(order.PaymentReceipts)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "采购单删除成功"})
}

// GetPurchaseMaterialsSummary 获取指定月份的采购汇总金额（仅统计已到货采购单）。
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
	if err := models.DB.QueryRow(`
        SELECT COALESCE(SUM(pm.amount), 0)
        FROM purchase_orders po
        LEFT JOIN purchase_materials pm ON pm.purchase_order_id = po.id
        WHERE po.status = 1 AND DATE_FORMAT(po.purchase_date, '%Y-%m') = ?
    `, month).Scan(&monthlyTotal); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var yearlyTotal float64
	if err := models.DB.QueryRow(`
        SELECT COALESCE(SUM(pm.amount), 0)
        FROM purchase_orders po
        LEFT JOIN purchase_materials pm ON pm.purchase_order_id = po.id
        WHERE po.status = 1 AND DATE_FORMAT(po.purchase_date, '%Y') = ?
    `, year).Scan(&yearlyTotal); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var monthlyFreight float64
	if err := models.DB.QueryRow("SELECT COALESCE(SUM(freight), 0) FROM purchase_orders WHERE status = 1 AND DATE_FORMAT(purchase_date, '%Y-%m') = ?", month).Scan(&monthlyFreight); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var yearlyFreight float64
	if err := models.DB.QueryRow("SELECT COALESCE(SUM(freight), 0) FROM purchase_orders WHERE status = 1 AND DATE_FORMAT(purchase_date, '%Y') = ?", year).Scan(&yearlyFreight); err != nil {
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

// UploadPurchaseReceipt 上传支付水单图片。
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
