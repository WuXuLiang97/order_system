package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"order-system/models"
	"order-system/utils"
	"strconv"
	"strings"
	"time"
)

type outboundItemReq struct {
	ProductID int     `json:"product_id"`
	Quantity  float64 `json:"quantity"`
}

type outboundCreateReq struct {
	OutDate  string            `json:"out_date"`
	Receiver string            `json:"receiver"`
	Remark   string            `json:"remark"`
	Items    []outboundItemReq `json:"items"`
}

// CreateProductOutbound 成品出库：扣减库存并生成送货单（可含多个型号）
func CreateProductOutbound(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req outboundCreateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.Items) == 0 {
		writeJSONError(w, http.StatusBadRequest, "请至少填写一行出库明细")
		return
	}

	// 出库日期：为空则默认今天
	outDateVal, err := parsePurchaseDate(req.OutDate)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "出库日期格式不正确")
		return
	}
	outDate := time.Now()
	if t, ok := outDateVal.(time.Time); ok {
		outDate = t
	}
	outDateStr := outDate.Format("2006-01-02")

	tx, err := models.DB.Begin()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()

	outboundNo := "FH" + time.Now().Format("20060102150405")
	user := CurrentUser(r)
	userID := 0
	if user != nil {
		userID = user.ID
	}

	// 明细快照：先逐行读取产品信息并校验库存
	type snap struct {
		productID int
		name      string
		spec      string
		unit      string
		qty       float64
	}
	var snaps []snap
	for _, it := range req.Items {
		if it.ProductID <= 0 {
			writeJSONError(w, http.StatusBadRequest, "请选择要出库的产品")
			return
		}
		if it.Quantity <= 0 {
			writeJSONError(w, http.StatusBadRequest, "出库数量必须大于 0")
			return
		}
		var name, spec, unit string
		var stock float64
		err := tx.QueryRow("SELECT name, spec, unit, stock FROM products WHERE id = ?", it.ProductID).
			Scan(&name, &spec, &unit, &stock)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("产品 ID %d 不存在", it.ProductID))
			return
		}
		if stock < it.Quantity {
			writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("“%s”库存不足：当前 %.3f，需出库 %.3f", name, stock, it.Quantity))
			return
		}
		snaps = append(snaps, snap{productID: it.ProductID, name: name, spec: spec, unit: unit, qty: it.Quantity})
	}

	res, err := tx.Exec(`
        INSERT INTO product_outbound (outbound_no, out_date, receiver, remark, created_by_user_id)
        VALUES (?, ?, ?, ?, ?)`, outboundNo, outDateStr, strings.TrimSpace(req.Receiver), strings.TrimSpace(req.Remark), userID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	outboundID, _ := res.LastInsertId()

	for _, s := range snaps {
		if _, err := tx.Exec(`
            INSERT INTO product_outbound_items (outbound_id, product_id, product_name, spec, unit, quantity)
            VALUES (?, ?, ?, ?, ?, ?)`, outboundID, s.productID, s.name, s.spec, s.unit, s.qty); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if _, err := tx.Exec("UPDATE products SET stock = stock - ? WHERE id = ?", s.qty, s.productID); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	if err := tx.Commit(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":          outboundID,
		"outbound_no": outboundNo,
		"message":     "成品出库成功，可打印送货单",
	})
}

// ListProductOutbounds 成品出库记录列表
func ListProductOutbounds(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit := 50
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 200 {
		limit = v
	}
	list, err := models.ListProductOutbounds(limit)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// DeleteProductOutbound 删除出库记录并回补库存
func DeleteProductOutbound(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || id <= 0 {
		writeJSONError(w, http.StatusBadRequest, "Invalid id")
		return
	}
	items, err := models.GetProductOutboundItems(id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	tx, err := models.DB.Begin()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	for _, it := range items {
		if _, err := tx.Exec("UPDATE products SET stock = stock + ? WHERE id = ?", it.Quantity, it.ProductID); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if _, err := tx.Exec("DELETE FROM product_outbound_items WHERE outbound_id = ?", id); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := tx.Exec("DELETE FROM product_outbound WHERE id = ?", id); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "出库记录已删除，库存已回补"})
}

// ProductOutboundPrintPage 送货单打印页（不含价格/金额，保护客户信息）
func ProductOutboundPrintPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || id <= 0 {
		http.Error(w, "Invalid outbound id", http.StatusBadRequest)
		return
	}

	// 主记录
	var ob models.ProductOutbound
	var outDate sql.NullTime
	err = models.DB.QueryRow(`
        SELECT o.id, o.outbound_no, o.out_date, o.receiver, o.remark,
               o.created_by_user_id, COALESCE(NULLIF(u.display_name, ''), u.username, '') AS created_by_name,
               o.created_at
        FROM product_outbound o
        LEFT JOIN users u ON u.id = o.created_by_user_id
        WHERE o.id = ?`, id).
		Scan(&ob.ID, &ob.OutboundNo, &outDate, &ob.Receiver, &ob.Remark,
			&ob.CreatedByUserID, &ob.CreatedByName, &ob.CreatedAt)
	if err != nil {
		http.Error(w, "出库记录不存在", http.StatusNotFound)
		return
	}
	if outDate.Valid {
		ob.OutDate = outDate.Time
	}

	items, err := models.GetProductOutboundItems(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	totalQuantity := 0.0
	for _, it := range items {
		totalQuantity += it.Quantity
	}

	data := struct {
		Outbound      models.ProductOutbound
		Items         []models.ProductOutboundItem
		Now           time.Time
		TotalQuantity float64
	}{
		Outbound:      ob,
		Items:         items,
		Now:           time.Now(),
		TotalQuantity: totalQuantity,
	}

	tmpl := template.Must(template.New("product_outbound.html").Funcs(utils.FuncMap()).ParseFiles("templates/product_outbound.html"))
	if err := tmpl.Execute(w, data); err != nil {
		log.Printf("Template execution error: %v", err)
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}
