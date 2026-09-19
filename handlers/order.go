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

type OrderCreateRequest struct {
	CustomerName         string            `json:"customer_name"`
	CustomerID           int               `json:"customer_id"`
	OwnerUserID          int               `json:"owner_user_id"`
	Region               string            `json:"region"`
	CustomerAddress      string            `json:"customer_address"`
	CustomerPhone        string            `json:"customer_phone"`
	Currency             string            `json:"currency"`
	TradeTerms           string            `json:"trade_terms"`
	ShippingMark         string            `json:"shipping_mark"`
	OrderDate            string            `json:"order_date"`
	ExpectedShippingDate string            `json:"expected_shipping_date"`
	CustomerRequiredDate string            `json:"customer_required_date"`
	LogisticsDays        int               `json:"logistics_days"`
	PaymentStatus        int               `json:"payment_status"`
	PreparedBy           string            `json:"prepared_by"`
	PaymentSettlement    string            `json:"payment_settlement"`
	FreightPayment       string            `json:"freight_payment"`
	FreightRecovery      string            `json:"freight_recovery"`
	TransportMethod      string            `json:"transport_method"`
	Remark               string            `json:"remark"`
	Items                []OrderCreateItem `json:"items"`
}

type OrderCreateItem struct {
	ProductID int      `json:"product_id"`
	Quantity  int      `json:"quantity"`
	Price     *float64 `json:"price"`
}

type orderItemView struct {
	ProductID         int     `json:"product_id"`
	ProductName       string  `json:"product_name"`
	Spec              string  `json:"spec"`
	Unit              string  `json:"unit"`
	Quantity          int     `json:"quantity"`
	Price             float64 `json:"price"`
	ShippedQuantity   float64 `json:"shipped_quantity"`
	RemainingQuantity float64 `json:"remaining_quantity"`
}

type orderListRow struct {
	models.Order
	Items []orderItemView `json:"items"`
}

// orderStockItem 需要调整库存的订单明细行（产品与数量）。
type orderStockItem struct {
	ProductID int
	Quantity  int
}

// CreateOrder 创建订单：只建立订单占用，不修改实际库存
func CreateOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req OrderCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.CustomerName == "" {
		http.Error(w, "Customer name is required", http.StatusBadRequest)
		return
	}
	if len(req.Items) == 0 {
		http.Error(w, "At least one product is required", http.StatusBadRequest)
		return
	}

	orderDate, err := parsePurchaseDate(req.OrderDate)
	if err != nil {
		http.Error(w, "下单日期格式不正确", http.StatusBadRequest)
		return
	}
	if orderDate == nil {
		orderDate = time.Now()
	}
	customerRequiredDate, err := parsePurchaseDate(req.CustomerRequiredDate)
	if err != nil {
		http.Error(w, "客户需求到货日格式不正确", http.StatusBadRequest)
		return
	}
	logisticsDays := req.LogisticsDays
	if logisticsDays < 0 {
		logisticsDays = 0
	}

	expectedShippingDate, err := parsePurchaseDate(req.ExpectedShippingDate)
	if err != nil {
		http.Error(w, "预计发货日期格式不正确", http.StatusBadRequest)
		return
	}

	tx, err := models.DB.Begin()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// 计算总金额。单价由客户端按客户报价传入；未传时兼容使用产品默认单价。
	itemPrices := make([]float64, len(req.Items))
	var total float64
	for i, item := range req.Items {
		if item.Quantity <= 0 {
			http.Error(w, "产品数量必须大于0", http.StatusBadRequest)
			return
		}
		var productPrice float64
		err := tx.QueryRow("SELECT price FROM products WHERE id = ?", item.ProductID).Scan(&productPrice)
		if err != nil {
			http.Error(w, fmt.Sprintf("Product ID %d not found", item.ProductID), http.StatusBadRequest)
			return
		}

		price := productPrice
		if item.Price != nil {
			price = *item.Price
		}
		if price < 0 {
			http.Error(w, "产品单价不能小于0", http.StatusBadRequest)
			return
		}
		itemPrices[i] = price
		total += price * float64(item.Quantity)
	}

	// 生成“YKL + 下单日期 + 4位当天序号”的订单号。
	numberDate, ok := orderDate.(time.Time)
	if !ok {
		numberDate = time.Now()
	}
	orderNo, err := models.NextOrderNoTx(tx, numberDate)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	preparedBy := strings.TrimSpace(req.PreparedBy)
	if preparedBy == "" {
		if user := CurrentUser(r); user != nil {
			preparedBy = user.DisplayName
			if preparedBy == "" {
				preparedBy = user.Username
			}
		}
	}
	paymentSettlement := orderOptionDefault(req.PaymentSettlement, "现付")
	freightPayment := orderOptionDefault(req.FreightPayment, "现付")
	freightRecovery := orderOptionDefault(req.FreightRecovery, "可回收")
	transportMethod := orderOptionDefault(req.TransportMethod, "物流")
	customerID := req.CustomerID
	if customerID < 0 {
		customerID = 0
	}
	currency := orderOptionDefault(req.Currency, "CNY")
	tradeTerms := strings.TrimSpace(req.TradeTerms)
	shippingMark := strings.TrimSpace(req.ShippingMark)

	ownerUserID := req.OwnerUserID
	if ownerUserID < 0 {
		ownerUserID = 0
	}
	if ownerUserID == 0 {
		if u := CurrentUser(r); u != nil {
			ownerUserID = u.ID
		}
	}

	var createdBy interface{} = nil
	if u := CurrentUser(r); u != nil {
		createdBy = u.ID
	}

	result, err := tx.Exec(`
        INSERT INTO orders
        (order_no, customer_name, region, customer_address, customer_phone,
         order_date, expected_shipping_date, customer_required_date, logistics_days, delivery_date, total_amount, status, payment_status,
         prepared_by, created_by_user_id, payment_settlement, freight_payment, freight_recovery, transport_method, remark,
         customer_id, owner_user_id, currency, trade_terms, shipping_mark)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, 0, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `, orderNo, req.CustomerName, req.Region, req.CustomerAddress, req.CustomerPhone,
		orderDate, expectedShippingDate, customerRequiredDate, logisticsDays, total, req.PaymentStatus, preparedBy, createdBy, paymentSettlement,
		freightPayment, freightRecovery, transportMethod, req.Remark, customerID, ownerUserID, currency, tradeTerms, shippingMark)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	orderID, _ := result.LastInsertId()

	// 下单只建立订单占用账，不修改实际库存；缺口由采购需求公式计算。
	reservedTotal := 0.0
	unreservedTotal := 0.0
	for i, item := range req.Items {
		price := itemPrices[i]
		result, err := tx.Exec("INSERT INTO order_items (order_id, product_id, quantity, price) VALUES (?, ?, ?, ?)",
			orderID, item.ProductID, item.Quantity, price)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		orderItemID, err := result.LastInsertId()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		reserved, _, err := models.ReserveProductForOrderTx(tx, int(orderID), int(orderItemID), item.ProductID, float64(item.Quantity))
		if err != nil {
			http.Error(w, fmt.Sprintf("Product ID %d reservation failed: %v", item.ProductID, err), http.StatusInternalServerError)
			return
		}
		reservedTotal += reserved
		unreservedTotal += float64(item.Quantity) - reserved

		// 保存下单时BOM快照，供历史订单追溯；当前生产需求按可用BOM计算。
		boms, err := models.GetBOMByProduct(item.ProductID)
		if err != nil {
			continue
		}
		for _, bom := range boms {
			usage := bom.Quantity * float64(item.Quantity)
			if _, err := tx.Exec("INSERT INTO order_bom_snapshot (order_id, product_id, raw_material_id, quantity) VALUES (?, ?, ?, ?)", orderID, item.ProductID, bom.RawMaterialID, usage); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
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
		"order_no":            orderNo,
		"total":               total,
		"message":             "订单创建成功",
		"reserved_quantity":   reservedTotal,
		"unreserved_quantity": unreservedTotal,
	})
}

// GetOrders 获取订单列表（支持状态筛选和搜索，返回订单及明细）
func GetOrders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	statusStr := r.URL.Query().Get("status")
	keyword := r.URL.Query().Get("keyword")
	user := CurrentUser(r)
	perms := PermissionsFor(user)
	viewAll := perms[PermOrderViewAll] || perms[PermOrderEditAll]

	query := `
        SELECT id, order_no, customer_name, region, customer_address, customer_phone,
               order_date, expected_shipping_date, customer_required_date, logistics_days, delivery_date, total_amount, status, payment_status,
               prepared_by, created_by_user_id, owner_user_id, payment_settlement, freight_payment, freight_recovery, transport_method,
               customer_id, currency, trade_terms, shipping_mark,
               created_at, COALESCE(remark, '') AS remark, COALESCE((SELECT COALESCE(NULLIF(u2.display_name, ''), u2.username, '') FROM users u2 WHERE u2.id = orders.owner_user_id), '') AS owner_name,
               COALESCE((SELECT c.code FROM customers c WHERE c.id = orders.customer_id), '') AS customer_code,
               COALESCE((SELECT c.full_name FROM customers c WHERE c.id = orders.customer_id), '') AS customer_full_name
        FROM orders
        WHERE 1=1
    `
	var args []interface{}

	if statusStr != "" {
		status, err := strconv.Atoi(statusStr)
		if err == nil && status >= 0 && status <= 4 {
			query += " AND status = ?"
			args = append(args, status)
		}
	}

	if keyword != "" {
		query += " AND (order_no LIKE ? OR customer_name LIKE ? OR region LIKE ?)"
		like := "%" + keyword + "%"
		args = append(args, like, like, like)
	}

	if !viewAll {
		if user != nil {
			query += " AND (created_by_user_id = ? OR owner_user_id = ?)"
			args = append(args, user.ID, user.ID)
		} else {
			query += " AND 1=0"
		}
	}

	query += " ORDER BY id DESC"

	rows, err := models.DB.Query(query, args...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var result []orderListRow
	for rows.Next() {
		var o models.Order
		var orderDate sql.NullTime
		var expectedDate sql.NullTime
		var deliveryDate sql.NullTime
		var requiredDate sql.NullTime
		var logisticsDays int
		var createdByUser sql.NullInt64
		err := rows.Scan(&o.ID, &o.OrderNo, &o.CustomerName, &o.Region, &o.CustomerAddress, &o.CustomerPhone,
			&orderDate, &expectedDate, &requiredDate, &logisticsDays, &deliveryDate, &o.TotalAmount, &o.Status, &o.PaymentStatus,
			&o.PreparedBy, &createdByUser, &o.OwnerUserID, &o.PaymentSettlement, &o.FreightPayment, &o.FreightRecovery, &o.TransportMethod,
			&o.CustomerID, &o.Currency, &o.TradeTerms, &o.ShippingMark, &o.CreatedAt, &o.Remark, &o.OwnerName, &o.CustomerCode, &o.CustomerFullName)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if orderDate.Valid {
			o.OrderDate = &orderDate.Time
		}
		if expectedDate.Valid {
			o.ExpectedShippingDate = &expectedDate.Time
		}
		if requiredDate.Valid {
			o.CustomerRequiredDate = &requiredDate.Time
		}
		o.LogisticsDays = logisticsDays
		if createdByUser.Valid {
			uid := int(createdByUser.Int64)
			o.CreatedByUserID = &uid
		}
		computeOrderWarning(&o, time.Now())
		if deliveryDate.Valid {
			o.DeliveryDate = &deliveryDate.Time
		}

		items, err := getOrderItems(o.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		result = append(result, orderListRow{Order: o, Items: items})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func getOrderItems(orderID int) ([]orderItemView, error) {
	rows, err := models.DB.Query(`
        SELECT oi.product_id, oi.quantity, oi.price, p.name AS product_name,
               COALESCE(p.spec, '') AS spec, COALESCE(p.unit, '') AS unit,
               COALESCE(SUM(poi.quantity), 0) AS shipped_quantity
        FROM order_items oi
        JOIN products p ON oi.product_id = p.id
        LEFT JOIN product_outbound_items poi ON poi.order_item_id = oi.id
        WHERE oi.order_id = ?
        GROUP BY oi.id, oi.product_id, oi.quantity, oi.price, p.name, p.spec, p.unit
    `, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []orderItemView
	for rows.Next() {
		var it orderItemView
		if err := rows.Scan(&it.ProductID, &it.Quantity, &it.Price, &it.ProductName, &it.Spec, &it.Unit, &it.ShippedQuantity); err != nil {
			return nil, err
		}
		it.RemainingQuantity = float64(it.Quantity) - it.ShippedQuantity
		if it.RemainingQuantity < 0 {
			it.RemainingQuantity = 0
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

type orderShipmentItemView struct {
	OrderItemID       int     `json:"order_item_id"`
	ProductID         int     `json:"product_id"`
	ProductName       string  `json:"product_name"`
	Spec              string  `json:"spec"`
	Unit              string  `json:"unit"`
	Quantity          float64 `json:"quantity"`
	ShippedQuantity   float64 `json:"shipped_quantity"`
	RemainingQuantity float64 `json:"remaining_quantity"`
}

type shippableOrderView struct {
	ID                   int        `json:"id"`
	OrderNo              string     `json:"order_no"`
	CustomerName         string     `json:"customer_name"`
	OrderDate            *time.Time `json:"order_date"`
	ExpectedShippingDate *time.Time `json:"expected_shipping_date"`
	Status               int        `json:"status"`
	ItemCount            int        `json:"item_count"`
	TotalQuantity        float64    `json:"total_quantity"`
	ShippedQuantity      float64    `json:"shipped_quantity"`
}

func loadOrderShipmentItems(orderID int) ([]orderShipmentItemView, error) {
	rows, err := models.DB.Query(`
        SELECT oi.id, oi.product_id, p.name, COALESCE(p.spec, ''), COALESCE(p.unit, ''),
               oi.quantity, COALESCE(SUM(poi.quantity), 0) AS shipped_quantity
        FROM order_items oi
        JOIN products p ON p.id = oi.product_id
        LEFT JOIN product_outbound_items poi ON poi.order_item_id = oi.id
        WHERE oi.order_id = ?
        GROUP BY oi.id, oi.product_id, p.name, p.spec, p.unit, oi.quantity
        ORDER BY oi.id ASC
    `, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []orderShipmentItemView
	for rows.Next() {
		var item orderShipmentItemView
		if err := rows.Scan(&item.OrderItemID, &item.ProductID, &item.ProductName, &item.Spec,
			&item.Unit, &item.Quantity, &item.ShippedQuantity); err != nil {
			return nil, err
		}
		item.RemainingQuantity = item.Quantity - item.ShippedQuantity
		if item.RemainingQuantity < 0 {
			item.RemainingQuantity = 0
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ListShippableOrders 获取可生成送货单的订单。
func ListShippableOrders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rows, err := models.DB.Query(`
        SELECT id, order_no, customer_name, order_date, expected_shipping_date, status
        FROM orders
        WHERE status IN (1, 2)
        ORDER BY id DESC
    `)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var list []shippableOrderView
	for rows.Next() {
		var order shippableOrderView
		var orderDate, expectedDate sql.NullTime
		if err := rows.Scan(&order.ID, &order.OrderNo, &order.CustomerName, &orderDate, &expectedDate, &order.Status); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if orderDate.Valid {
			order.OrderDate = &orderDate.Time
		}
		if expectedDate.Valid {
			order.ExpectedShippingDate = &expectedDate.Time
		}
		items, err := loadOrderShipmentItems(order.ID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, item := range items {
			order.ItemCount++
			order.TotalQuantity += item.Quantity
			order.ShippedQuantity += item.ShippedQuantity
		}
		if order.TotalQuantity > order.ShippedQuantity+0.0005 {
			list = append(list, order)
		}
	}
	if err := rows.Err(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// GetOrderShipmentItems 获取订单的可发货明细与剩余数量。
func GetOrderShipmentItems(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	orderID, err := strconv.Atoi(r.URL.Query().Get("order_id"))
	if err != nil || orderID <= 0 {
		writeJSONError(w, http.StatusBadRequest, "Invalid order_id")
		return
	}

	var orderNo, customerName, freightPayment string
	var status int
	err = models.DB.QueryRow(`
        SELECT order_no, customer_name, status, COALESCE(freight_payment, '')
        FROM orders
        WHERE id = ?
    `, orderID).Scan(&orderNo, &customerName, &status, &freightPayment)
	if err == sql.ErrNoRows {
		writeJSONError(w, http.StatusNotFound, "订单不存在")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if status != 1 && status != 2 {
		writeJSONError(w, http.StatusBadRequest, "当前订单状态不能生成送货单")
		return
	}

	items, err := loadOrderShipmentItems(orderID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	defaultSettlement := "现金"
	if strings.TrimSpace(freightPayment) == "到付" {
		defaultSettlement = "到付"
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"order_id":                  orderID,
		"order_no":                  orderNo,
		"customer_name":             customerName,
		"status":                    status,
		"default_settlement_method": defaultSettlement,
		"items":                     items,
	})
}

// customerOrderRow 客户详情页展示的关联订单行
type customerOrderRow struct {
	ID            int        `json:"id"`
	OrderNo       string     `json:"order_no"`
	OrderDate     *time.Time `json:"order_date"`
	TotalAmount   float64    `json:"total_amount"`
	Currency      string     `json:"currency"`
	Status        int        `json:"status"`
	PaymentStatus int        `json:"payment_status"`
	CreatedAt     time.Time  `json:"created_at"`
}

// GetCustomerOrders 获取某客户的关联订单列表（按订单行级权限过滤）
func GetCustomerOrders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	customerID, err := strconv.Atoi(r.URL.Query().Get("customer_id"))
	if err != nil || customerID <= 0 {
		writeJSONError(w, http.StatusBadRequest, "Invalid customer_id")
		return
	}
	customer, err := models.GetCustomerByID(customerID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "客户不存在")
		return
	}
	user := CurrentUser(r)
	if !canViewCustomer(user, customer) {
		writeJSONError(w, http.StatusForbidden, "无权查看该客户")
		return
	}

	perms := PermissionsFor(user)
	viewAll := perms[PermOrderViewAll] || perms[PermOrderEditAll]
	query := `
        SELECT id, order_no, order_date, total_amount, currency, status, payment_status, created_at
        FROM orders
        WHERE customer_id = ?`
	var args []interface{}
	args = append(args, customerID)
	if !viewAll {
		if user != nil {
			query += " AND (created_by_user_id = ? OR owner_user_id = ?)"
			args = append(args, user.ID, user.ID)
		} else {
			query += " AND 1=0"
		}
	}
	query += " ORDER BY id DESC"

	rows, err := models.DB.Query(query, args...)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var list []customerOrderRow
	for rows.Next() {
		var row customerOrderRow
		var orderDate sql.NullTime
		if err := rows.Scan(&row.ID, &row.OrderNo, &orderDate, &row.TotalAmount, &row.Currency, &row.Status, &row.PaymentStatus, &row.CreatedAt); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if orderDate.Valid {
			row.OrderDate = &orderDate.Time
		}
		list = append(list, row)
	}
	if err := rows.Err(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// GetOrderDetail 获取订单详情（JSON）
func GetOrderDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || id <= 0 {
		http.Error(w, "Invalid id", http.StatusBadRequest)
		return
	}

	var order models.Order
	var orderDate sql.NullTime
	var expectedDate sql.NullTime
	var deliveryDate sql.NullTime
	var requiredDate sql.NullTime
	var logisticsDays int
	var createdByUser sql.NullInt64
	user := CurrentUser(r)
	err = models.DB.QueryRow(`
        SELECT id, order_no, customer_name, region, customer_address, customer_phone,
               order_date, expected_shipping_date, customer_required_date, logistics_days, delivery_date, total_amount, status, payment_status,
               prepared_by, created_by_user_id, owner_user_id, payment_settlement, freight_payment, freight_recovery, transport_method,
               customer_id, currency, trade_terms, shipping_mark,
               created_at, COALESCE(remark, '') AS remark, COALESCE((SELECT COALESCE(NULLIF(u2.display_name, ''), u2.username, '') FROM users u2 WHERE u2.id = orders.owner_user_id), '') AS owner_name,
               COALESCE((SELECT c.code FROM customers c WHERE c.id = orders.customer_id), '') AS customer_code,
               COALESCE((SELECT c.full_name FROM customers c WHERE c.id = orders.customer_id), '') AS customer_full_name
        FROM orders WHERE id = ?
    `, id).Scan(&order.ID, &order.OrderNo, &order.CustomerName, &order.Region, &order.CustomerAddress, &order.CustomerPhone,
		&orderDate, &expectedDate, &requiredDate, &logisticsDays, &deliveryDate, &order.TotalAmount, &order.Status, &order.PaymentStatus,
		&order.PreparedBy, &createdByUser, &order.OwnerUserID, &order.PaymentSettlement, &order.FreightPayment, &order.FreightRecovery, &order.TransportMethod,
		&order.CustomerID, &order.Currency, &order.TradeTerms, &order.ShippingMark, &order.CreatedAt, &order.Remark, &order.OwnerName, &order.CustomerCode, &order.CustomerFullName)
	if err != nil {
		http.Error(w, "Order not found", http.StatusNotFound)
		return
	}
	if orderDate.Valid {
		order.OrderDate = &orderDate.Time
	}
	if expectedDate.Valid {
		order.ExpectedShippingDate = &expectedDate.Time
	}
	if requiredDate.Valid {
		order.CustomerRequiredDate = &requiredDate.Time
	}
	order.LogisticsDays = logisticsDays
	if createdByUser.Valid {
		uid := int(createdByUser.Int64)
		order.CreatedByUserID = &uid
	}
	computeOrderWarning(&order, time.Now())
	if !canViewOrder(user, order.CreatedByUserID, &order.OwnerUserID) {
		http.Error(w, "Order not found", http.StatusNotFound)
		return
	}
	if deliveryDate.Valid {
		order.DeliveryDate = &deliveryDate.Time
	}

	items, err := getOrderItems(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"order": order,
		"items": items,
	})
}

// DeleteOrder 删除订单（先归还创建订单时扣减的成品库存和原材料库存，再删除订单及明细）
func DeleteOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || id <= 0 {
		http.Error(w, "Invalid id", http.StatusBadRequest)
		return
	}

	tx, err := models.DB.Begin()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// 锁定订单行并读取当前状态，避免并发删除/取消时库存被重复归还。
	var status int
	err = tx.QueryRow("SELECT status FROM orders WHERE id = ? FOR UPDATE", id).Scan(&status)
	if err == sql.ErrNoRows {
		http.Error(w, "Order not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var outboundCount int
	if err := tx.QueryRow("SELECT COUNT(*) FROM product_outbound WHERE order_id = ?", id).Scan(&outboundCount); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if outboundCount > 0 {
		http.Error(w, "该订单已有关联送货单，请先删除送货单后再删除订单", http.StatusBadRequest)
		return
	}

	// 已取消（status=4）的订单在取消时已归还过库存，删除时不再重复归还。
	if status != 4 {
		if err := restoreOrderStock(tx, id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if _, err := tx.Exec("DELETE FROM inventory_reservations WHERE order_id = ?", id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 删除订单明细与订单主记录。
	if _, err := tx.Exec("DELETE FROM order_items WHERE order_id = ?", id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := tx.Exec("DELETE FROM orders WHERE id = ?", id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Order deleted successfully"})
}

// restoreOrderStock 取消/删除订单时只释放订单占用，不修改实际库存。
func restoreOrderStock(tx *sql.Tx, orderID int) error {
	return models.ReleaseOrderReservationsTx(tx, orderID)
}

// deductOrderStock 恢复已取消订单时，按当前可用库存重新建立订单占用。
func deductOrderStock(tx *sql.Tx, orderID int) error {
	return models.ReactivateOrderReservationsTx(tx, orderID)
}

// UpdateOrder 更新订单信息（不修改明细）
func UpdateOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ID                   int    `json:"id"`
		CustomerName         string `json:"customer_name"`
		CustomerID           int    `json:"customer_id"`
		OwnerUserID          int    `json:"owner_user_id"`
		Region               string `json:"region"`
		Currency             string `json:"currency"`
		TradeTerms           string `json:"trade_terms"`
		ShippingMark         string `json:"shipping_mark"`
		OrderDate            string `json:"order_date"`
		ExpectedShippingDate string `json:"expected_shipping_date"`
		CustomerRequiredDate string `json:"customer_required_date"`
		LogisticsDays        int    `json:"logistics_days"`
		PaymentStatus        int    `json:"payment_status"`
		PreparedBy           string `json:"prepared_by"`
		PaymentSettlement    string `json:"payment_settlement"`
		FreightPayment       string `json:"freight_payment"`
		FreightRecovery      string `json:"freight_recovery"`
		TransportMethod      string `json:"transport_method"`
		Remark               string `json:"remark"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.ID <= 0 {
		http.Error(w, "Invalid order ID", http.StatusBadRequest)
		return
	}
	if req.CustomerName == "" {
		http.Error(w, "Customer name is required", http.StatusBadRequest)
		return
	}

	orderDate, err := parsePurchaseDate(req.OrderDate)
	if err != nil {
		http.Error(w, "下单日期格式不正确", http.StatusBadRequest)
		return
	}
	customerRequiredDate, err := parsePurchaseDate(req.CustomerRequiredDate)
	if err != nil {
		http.Error(w, "客户需求到货日格式不正确", http.StatusBadRequest)
		return
	}
	logisticsDays := req.LogisticsDays
	if logisticsDays < 0 {
		logisticsDays = 0
	}

	expectedDate, err := parsePurchaseDate(req.ExpectedShippingDate)
	if err != nil {
		http.Error(w, "预计发货日期格式不正确", http.StatusBadRequest)
		return
	}

	preparedBy := strings.TrimSpace(req.PreparedBy)
	if preparedBy == "" {
		if user := CurrentUser(r); user != nil {
			preparedBy = user.DisplayName
			if preparedBy == "" {
				preparedBy = user.Username
			}
		}
	}
	paymentSettlement := orderOptionDefault(req.PaymentSettlement, "现付")
	freightPayment := orderOptionDefault(req.FreightPayment, "现付")
	freightRecovery := orderOptionDefault(req.FreightRecovery, "可回收")
	transportMethod := orderOptionDefault(req.TransportMethod, "物流")
	customerID := req.CustomerID
	if customerID < 0 {
		customerID = 0
	}
	currency := orderOptionDefault(req.Currency, "CNY")
	tradeTerms := strings.TrimSpace(req.TradeTerms)
	shippingMark := strings.TrimSpace(req.ShippingMark)

	ownerUserID := req.OwnerUserID
	if ownerUserID < 0 {
		ownerUserID = 0
	}

	var orderCreatedBy sql.NullInt64
	var orderOwner sql.NullInt64
	err = models.DB.QueryRow("SELECT created_by_user_id, owner_user_id FROM orders WHERE id = ?", req.ID).Scan(&orderCreatedBy, &orderOwner)
	if err == sql.ErrNoRows {
		http.Error(w, "Order not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !canEditOrder(CurrentUser(r), nullIntPtr(orderCreatedBy), nullIntPtr(orderOwner)) {
		writeJSONError(w, http.StatusForbidden, "没有权限修改该订单")
		return
	}

	_, err = models.DB.Exec(`
        UPDATE orders SET
            customer_name = ?,
            customer_id = ?,
            owner_user_id = ?,
            region = ?,
            currency = ?,
            trade_terms = ?,
            shipping_mark = ?,
            order_date = ?,
            expected_shipping_date = ?,
            customer_required_date = ?,
            logistics_days = ?,
            payment_status = ?,
            prepared_by = ?,
            payment_settlement = ?,
            freight_payment = ?,
            freight_recovery = ?,
            transport_method = ?,
            remark = ?
        WHERE id = ?
    `, req.CustomerName, customerID, ownerUserID, req.Region, currency, tradeTerms, shippingMark, orderDate, expectedDate, customerRequiredDate, logisticsDays, req.PaymentStatus, preparedBy, paymentSettlement, freightPayment, freightRecovery, transportMethod, req.Remark, req.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Order updated successfully"})
}

// OrderDetailPage 渲染订单详情页面（用于打印）
func OrderDetailPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || id <= 0 {
		http.Error(w, "Invalid order id", http.StatusBadRequest)
		return
	}

	var order models.Order
	var orderDate sql.NullTime
	var expectedDate sql.NullTime
	var deliveryDate sql.NullTime
	var requiredDate sql.NullTime
	var logisticsDays int
	var createdByUser sql.NullInt64
	user := CurrentUser(r)
	err = models.DB.QueryRow(`
        SELECT id, order_no, customer_name, region, customer_address, customer_phone,
               order_date, expected_shipping_date, customer_required_date, logistics_days, delivery_date, total_amount, status, payment_status,
               prepared_by, created_by_user_id, owner_user_id, payment_settlement, freight_payment, freight_recovery, transport_method,
               customer_id, currency, trade_terms, shipping_mark,
               created_at, COALESCE(remark, '') AS remark, COALESCE((SELECT COALESCE(NULLIF(u2.display_name, ''), u2.username, '') FROM users u2 WHERE u2.id = orders.owner_user_id), '') AS owner_name,
               COALESCE((SELECT c.code FROM customers c WHERE c.id = orders.customer_id), '') AS customer_code,
               COALESCE((SELECT c.full_name FROM customers c WHERE c.id = orders.customer_id), '') AS customer_full_name
        FROM orders WHERE id = ?
    `, id).Scan(&order.ID, &order.OrderNo, &order.CustomerName, &order.Region, &order.CustomerAddress, &order.CustomerPhone,
		&orderDate, &expectedDate, &requiredDate, &logisticsDays, &deliveryDate, &order.TotalAmount, &order.Status, &order.PaymentStatus,
		&order.PreparedBy, &createdByUser, &order.OwnerUserID, &order.PaymentSettlement, &order.FreightPayment, &order.FreightRecovery, &order.TransportMethod,
		&order.CustomerID, &order.Currency, &order.TradeTerms, &order.ShippingMark, &order.CreatedAt, &order.Remark, &order.OwnerName, &order.CustomerCode, &order.CustomerFullName)
	if err != nil {
		http.Error(w, "Order not found", http.StatusNotFound)
		return
	}
	if orderDate.Valid {
		order.OrderDate = &orderDate.Time
	}
	if expectedDate.Valid {
		order.ExpectedShippingDate = &expectedDate.Time
	}
	if requiredDate.Valid {
		order.CustomerRequiredDate = &requiredDate.Time
	}
	order.LogisticsDays = logisticsDays
	if createdByUser.Valid {
		uid := int(createdByUser.Int64)
		order.CreatedByUserID = &uid
	}
	computeOrderWarning(&order, time.Now())
	if !canViewOrder(user, order.CreatedByUserID, &order.OwnerUserID) {
		http.Error(w, "Order not found", http.StatusNotFound)
		return
	}
	if deliveryDate.Valid {
		order.DeliveryDate = &deliveryDate.Time
	}

	rows, err := models.DB.Query(`
        SELECT oi.product_id, oi.quantity, oi.price, p.name AS product_name,
               COALESCE(p.spec, '') AS spec, COALESCE(p.unit, '') AS unit
        FROM order_items oi
        JOIN products p ON oi.product_id = p.id
        WHERE oi.order_id = ?
    `, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type Item struct {
		ProductID   int
		ProductName string
		Spec        string
		Unit        string
		Quantity    int
		Price       float64
	}
	var items []Item
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ProductID, &it.Quantity, &it.Price, &it.ProductName, &it.Spec, &it.Unit); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		items = append(items, it)
	}
	if err = rows.Err(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	totalQuantity := 0
	for _, it := range items {
		totalQuantity += it.Quantity
	}

	outbounds, err := models.GetProductOutboundsByOrder(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := struct {
		Order         models.Order
		Items         []Item
		Outbounds     []models.ProductOutbound
		Now           time.Time
		TotalQuantity int
	}{
		Order:         order,
		Items:         items,
		Outbounds:     outbounds,
		Now:           time.Now(),
		TotalQuantity: totalQuantity,
	}

	tmpl := template.Must(template.New("order_detail.html").Funcs(utils.FuncMap()).ParseFiles("templates/order_detail.html"))
	err = tmpl.Execute(w, data)
	if err != nil {
		log.Printf("Template execution error: %v", err)
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

// orderOptionDefault 货款结算/运费等下拉字段为空时使用默认值。
func orderOptionDefault(v, fallback string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return fallback
	}
	return v
}

// 预警阈值（可调整）：按“距客户需求到货日还剩几天”判断，距离 = 客户需求到货日 - 今天（负数表示已超过）。
const (
	// warnOrangeDays 距需求到货日 <= 该天数时显示“即将逾期”。
	warnOrangeDays = 2
	// warnYellowDays 距需求到货日 <= 该天数时显示“到货风险”。
	warnYellowDays = 5
)

// computeOrderWarning 按“距客户需求到货日剩余天数”计算预警等级，并兼顾“预计到货日（预计发货日+物流天数）晚于需求到货日”的排产风险。
// 优先级：严重逾期(red) > 即将逾期(orange) > 到货风险(yellow) > 正常(blue)。
// 已取消订单不预警；已发货订单不再按剩余天数报红/橙，只评估到货是否准时。
func computeOrderWarning(o *models.Order, today time.Time) {
	o.WarningLevel = ""
	o.WarningLabel = ""
	if o == nil || o.Status == 4 || o.CustomerRequiredDate == nil {
		return
	}
	shipped := o.Status == 3
	T := calendarDay(today)
	R := calendarDay(*o.CustomerRequiredDate)
	left := daysBetween(R, T) // 距客户需求到货日剩余天数（负数=已超过）
	if !shipped {
		switch {
		case left < 0:
			o.WarningLevel, o.WarningLabel = "red", "严重逾期"
			return
		case left <= warnOrangeDays:
			o.WarningLevel, o.WarningLabel = "orange", "即将逾期"
			return
		case left <= warnYellowDays:
			o.WarningLevel, o.WarningLabel = "yellow", "到货风险"
			return
		}
	}
	if o.ExpectedShippingDate != nil {
		S := calendarDay(*o.ExpectedShippingDate)
		if S.AddDate(0, 0, o.LogisticsDays).After(R) { // 预计到货日 > 需求到货日
			o.WarningLevel, o.WarningLabel = "yellow", "到货风险"
			return
		}
	}
	o.WarningLevel, o.WarningLabel = "blue", "正常"
}

// calendarDay 将时间归一到 UTC 零点，仅保留“日历日期”以消除时区差异，便于按天比较。
func calendarDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// daysBetween 返回 a - b 相差的日历天数。
func daysBetween(a, b time.Time) int {
	return int(calendarDay(a).Sub(calendarDay(b)).Hours() / 24)
}

// nullIntPtr 将 sql.NullInt64 转为 *int（空值返回 nil）。
func nullIntPtr(n sql.NullInt64) *int {
	if !n.Valid {
		return nil
	}
	v := int(n.Int64)
	return &v
}
