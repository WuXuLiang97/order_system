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
	OrderItemID int     `json:"order_item_id"`
	ProductID   int     `json:"product_id"`
	Quantity    float64 `json:"quantity"`
}

type outboundCreateReq struct {
	OutDate          string            `json:"out_date"`
	Receiver         string            `json:"receiver"`
	SettlementMethod string            `json:"settlement_method"`
	OrderID          int               `json:"order_id"`
	Remark           string            `json:"remark"`
	Items            []outboundItemReq `json:"items"`
}

type outboundItemSnapshot struct {
	OrderItemID     int
	ProductID       int
	ProductName     string
	Spec            string
	Unit            string
	Quantity        float64
	OrderedQuantity float64
}

type productStockInfo struct {
	Name  string
	Spec  string
	Unit  string
	Stock float64
}

func getProductStockInfoTx(tx *sql.Tx, productID int) (productStockInfo, error) {
	var info productStockInfo
	err := tx.QueryRow("SELECT name, spec, unit, stock FROM products WHERE id = ? FOR UPDATE", productID).
		Scan(&info.Name, &info.Spec, &info.Unit, &info.Stock)
	return info, err
}

func getOrderItemShippedTx(tx *sql.Tx, orderItemID int) (float64, error) {
	var shipped float64
	err := tx.QueryRow(`
        SELECT COALESCE(SUM(poi.quantity), 0)
        FROM product_outbound_items poi
        WHERE poi.order_item_id = ?
    `, orderItemID).Scan(&shipped)
	return shipped, err
}

// refreshOrderShipmentStatusTx 按送货单明细重新计算订单的发货状态。
// 全部发完时状态改为已发货；部分发货或全部撤销时保持待发货。
func refreshOrderShipmentStatusTx(tx *sql.Tx, orderID int, shippedDate time.Time) error {
	rows, err := tx.Query(`
        SELECT oi.id, oi.quantity, COALESCE(SUM(poi.quantity), 0) AS shipped_quantity
        FROM order_items oi
        LEFT JOIN product_outbound_items poi ON poi.order_item_id = oi.id
        WHERE oi.order_id = ?
        GROUP BY oi.id, oi.quantity
    `, orderID)
	if err != nil {
		return err
	}
	defer rows.Close()

	totalOrdered := 0.0
	totalShipped := 0.0
	for rows.Next() {
		var itemID int
		var ordered, shipped float64
		if err := rows.Scan(&itemID, &ordered, &shipped); err != nil {
			return err
		}
		totalOrdered += ordered
		totalShipped += shipped
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if totalOrdered <= 0 {
		return nil
	}

	if totalShipped+0.0005 >= totalOrdered {
		_, err = tx.Exec(`
            UPDATE orders
            SET status = 3, shipped_date = COALESCE(shipped_date, ?)
            WHERE id = ? AND status <> 4
        `, shippedDate, orderID)
		return err
	}

	_, err = tx.Exec(`
        UPDATE orders
        SET status = 2, shipped_date = NULL
        WHERE id = ? AND status <> 4
    `, orderID)
	return err
}

// CreateProductOutbound 成品出库：扣减库存并生成送货单（可含多个型号）。
// 传 order_id 时，明细必须来自该订单，并校验已发数量与剩余数量。
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

	settlementMethod := strings.TrimSpace(req.SettlementMethod)
	if settlementMethod == "" {
		settlementMethod = "现金"
	}
	if settlementMethod != "现金" && settlementMethod != "到付" {
		writeJSONError(w, http.StatusBadRequest, "结算方式不正确")
		return
	}

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

	user := CurrentUser(r)
	userID := 0
	if user != nil {
		userID = user.ID
	}

	orderNo := ""
	receiver := strings.TrimSpace(req.Receiver)
	if req.OrderID > 0 {
		var orderStatus int
		var orderCustomer string
		err := tx.QueryRow(`
            SELECT order_no, customer_name, status
            FROM orders
            WHERE id = ?
            FOR UPDATE
        `, req.OrderID).Scan(&orderNo, &orderCustomer, &orderStatus)
		if err == sql.ErrNoRows {
			writeJSONError(w, http.StatusBadRequest, "关联订单不存在")
			return
		}
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if orderStatus == 4 {
			writeJSONError(w, http.StatusBadRequest, "已取消订单不能生成送货单")
			return
		}
		if orderStatus != 1 && orderStatus != 2 {
			writeJSONError(w, http.StatusBadRequest, "当前订单状态不能生成送货单")
			return
		}
		if receiver == "" {
			receiver = orderCustomer
		}
	}

	snaps := make([]outboundItemSnapshot, 0, len(req.Items))
	productInfoCache := make(map[int]productStockInfo)
	requestedByProduct := make(map[int]float64)
	seenOrderItems := make(map[int]bool)

	for _, it := range req.Items {
		if it.ProductID <= 0 {
			writeJSONError(w, http.StatusBadRequest, "请选择要出库的产品")
			return
		}
		if it.Quantity <= 0 {
			writeJSONError(w, http.StatusBadRequest, "出库数量必须大于 0")
			return
		}

		orderedQuantity := 0.0
		if req.OrderID > 0 {
			if it.OrderItemID <= 0 {
				writeJSONError(w, http.StatusBadRequest, "送货明细缺少订单明细关联")
				return
			}
			if seenOrderItems[it.OrderItemID] {
				writeJSONError(w, http.StatusBadRequest, "同一订单明细不能重复提交")
				return
			}
			seenOrderItems[it.OrderItemID] = true

			var itemProductID int
			var itemOrderedQuantity float64
			err := tx.QueryRow(`
                SELECT product_id, quantity
                FROM order_items
                WHERE id = ? AND order_id = ?
                FOR UPDATE
            `, it.OrderItemID, req.OrderID).Scan(&itemProductID, &itemOrderedQuantity)
			if err == sql.ErrNoRows {
				writeJSONError(w, http.StatusBadRequest, "订单明细不存在或不属于该订单")
				return
			}
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if itemProductID != it.ProductID {
				writeJSONError(w, http.StatusBadRequest, "订单明细与产品不一致")
				return
			}

			shippedQuantity, err := getOrderItemShippedTx(tx, it.OrderItemID)
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
			remaining := itemOrderedQuantity - shippedQuantity
			if remaining <= 0 {
				writeJSONError(w, http.StatusBadRequest, "订单明细已全部发货")
				return
			}
			if it.Quantity > remaining+0.0005 {
				writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("本次发货数量超过剩余数量，剩余 %.3f", remaining))
				return
			}
			orderedQuantity = itemOrderedQuantity
		}

		info, ok := productInfoCache[it.ProductID]
		if !ok {
			info, err = getProductStockInfoTx(tx, it.ProductID)
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("产品 ID %d 不存在", it.ProductID))
				return
			}
			productInfoCache[it.ProductID] = info
		}
		requestedByProduct[it.ProductID] += it.Quantity
		if requestedByProduct[it.ProductID] > info.Stock+0.0005 {
			writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("“%s”库存不足：当前 %.3f，需出库 %.3f", info.Name, info.Stock, requestedByProduct[it.ProductID]))
			return
		}

		snaps = append(snaps, outboundItemSnapshot{
			OrderItemID:     it.OrderItemID,
			ProductID:       it.ProductID,
			ProductName:     info.Name,
			Spec:            info.Spec,
			Unit:            info.Unit,
			Quantity:        it.Quantity,
			OrderedQuantity: orderedQuantity,
		})
	}

	outboundNo := "FH" + time.Now().Format("20060102150405")
	result, err := tx.Exec(`
        INSERT INTO product_outbound
        (outbound_no, out_date, receiver, settlement_method, order_id, order_no, remark, created_by_user_id)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)
    `, outboundNo, outDateStr, receiver, settlementMethod, req.OrderID, orderNo, strings.TrimSpace(req.Remark), userID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	outboundID, _ := result.LastInsertId()

	for _, item := range snaps {
		if _, err := tx.Exec(`
            INSERT INTO product_outbound_items
            (outbound_id, order_item_id, product_id, product_name, spec, unit, quantity, ordered_quantity)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?)
        `, outboundID, item.OrderItemID, item.ProductID, item.ProductName, item.Spec, item.Unit, item.Quantity, item.OrderedQuantity); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if _, err := tx.Exec("UPDATE products SET stock = stock - ? WHERE id = ?", item.Quantity, item.ProductID); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	if req.OrderID > 0 {
		if err := refreshOrderShipmentStatusTx(tx, req.OrderID, outDate); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	if err := tx.Commit(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	message := "成品出库成功，可打印送货单"
	if req.OrderID > 0 {
		message = "订单送货单生成成功，可打印"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":          outboundID,
		"outbound_no": outboundNo,
		"message":     message,
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

// ListProductOutboundsByOrder 获取订单关联的送货单。
func ListProductOutboundsByOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	orderID, err := strconv.Atoi(r.URL.Query().Get("order_id"))
	if err != nil || orderID <= 0 {
		writeJSONError(w, http.StatusBadRequest, "Invalid order_id")
		return
	}
	list, err := models.GetProductOutboundsByOrder(orderID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// DeleteProductOutbound 删除出库记录并回补库存，同时重新计算订单发货状态。
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

	tx, err := models.DB.Begin()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()

	var orderID int
	err = tx.QueryRow("SELECT order_id FROM product_outbound WHERE id = ? FOR UPDATE", id).Scan(&orderID)
	if err == sql.ErrNoRows {
		writeJSONError(w, http.StatusNotFound, "出库记录不存在")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	rows, err := tx.Query("SELECT product_id, quantity FROM product_outbound_items WHERE outbound_id = ?", id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type itemStock struct {
		productID int
		quantity  float64
	}
	var items []itemStock
	for rows.Next() {
		var it itemStock
		if err := rows.Scan(&it.productID, &it.quantity); err != nil {
			rows.Close()
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		items = append(items, it)
	}
	if err := rows.Close(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	for _, item := range items {
		if _, err := tx.Exec("UPDATE products SET stock = stock + ? WHERE id = ?", item.quantity, item.productID); err != nil {
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
	if orderID > 0 {
		if err := refreshOrderShipmentStatusTx(tx, orderID, time.Now()); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
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

	var ob models.ProductOutbound
	var outDate sql.NullTime
	err = models.DB.QueryRow(`
        SELECT o.id, o.outbound_no, o.out_date, o.receiver,
               COALESCE(NULLIF(o.settlement_method, ''), '现金') AS settlement_method,
               o.order_id, o.order_no, o.remark, o.created_by_user_id,
               COALESCE(NULLIF(u.display_name, ''), u.username, '') AS created_by_name,
               o.created_at
        FROM product_outbound o
        LEFT JOIN users u ON u.id = o.created_by_user_id
        WHERE o.id = ?`, id).
		Scan(&ob.ID, &ob.OutboundNo, &outDate, &ob.Receiver, &ob.SettlementMethod,
			&ob.OrderID, &ob.OrderNo, &ob.Remark, &ob.CreatedByUserID, &ob.CreatedByName, &ob.CreatedAt)
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
