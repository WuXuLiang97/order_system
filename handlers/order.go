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
	CustomerName         string `json:"customer_name"`
	Region               string `json:"region"`
	CustomerAddress      string `json:"customer_address"`
	CustomerPhone        string `json:"customer_phone"`
	OrderDate            string `json:"order_date"`
	ExpectedShippingDate string `json:"expected_shipping_date"`
	PaymentStatus        int    `json:"payment_status"`
	PreparedBy           string `json:"prepared_by"`
	PaymentSettlement    string `json:"payment_settlement"`
	FreightPayment       string `json:"freight_payment"`
	FreightRecovery      string `json:"freight_recovery"`
	TransportMethod      string `json:"transport_method"`
	Remark               string `json:"remark"`
	Items                []struct {
		ProductID int `json:"product_id"`
		Quantity  int `json:"quantity"`
	} `json:"items"`
}

type orderItemView struct {
	ProductID   int     `json:"product_id"`
	ProductName string  `json:"product_name"`
	Spec        string  `json:"spec"`
	Unit        string  `json:"unit"`
	Quantity    int     `json:"quantity"`
	Price       float64 `json:"price"`
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

// CreateOrder 创建订单（允许负库存，扣减成品和原材料）
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

	// 计算总金额
	var total float64
	for _, item := range req.Items {
		var price float64
		err := tx.QueryRow("SELECT price FROM products WHERE id = ?", item.ProductID).Scan(&price)
		if err != nil {
			http.Error(w, fmt.Sprintf("Product ID %d not found", item.ProductID), http.StatusBadRequest)
			return
		}
		total += price * float64(item.Quantity)
	}

	// 生成订单号
	orderNo := "ORD" + time.Now().Format("20060102150405")

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

	result, err := tx.Exec(`
        INSERT INTO orders
        (order_no, customer_name, region, customer_address, customer_phone,
         order_date, expected_shipping_date, delivery_date, total_amount, status, payment_status,
         prepared_by, payment_settlement, freight_payment, freight_recovery, transport_method, remark)
        VALUES (?, ?, ?, ?, ?, ?, ?, NULL, ?, 0, ?, ?, ?, ?, ?, ?, ?)
    `, orderNo, req.CustomerName, req.Region, req.CustomerAddress, req.CustomerPhone,
		orderDate, expectedShippingDate, total, req.PaymentStatus, preparedBy, paymentSettlement,
		freightPayment, freightRecovery, transportMethod, req.Remark)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	orderID, _ := result.LastInsertId()

	// 插入订单明细、扣减成品库存、收集原材料扣减量
	rawMaterialDeductions := make(map[int]float64)

	for _, item := range req.Items {
		var price float64
		err := tx.QueryRow("SELECT price FROM products WHERE id = ?", item.ProductID).Scan(&price)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		_, err = tx.Exec("INSERT INTO order_items (order_id, product_id, quantity, price) VALUES (?, ?, ?, ?)",
			orderID, item.ProductID, item.Quantity, price)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// 扣减成品库存（允许负数）
		_, err = tx.Exec("UPDATE products SET stock = stock - ? WHERE id = ?", item.Quantity, item.ProductID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// 获取产品BOM：累加原材料扣减量，并把用量写入订单BOM快照，
		// 供取消/恢复/删除订单时按快照归还或扣减（避免后续修改BOM影响历史订单）。
		boms, err := models.GetBOMByProduct(item.ProductID)
		if err != nil {
			continue
		}
		for _, bom := range boms {
			usage := bom.Quantity * float64(item.Quantity)
			rawMaterialDeductions[bom.RawMaterialID] += usage
			if _, err := tx.Exec("INSERT INTO order_bom_snapshot (order_id, product_id, raw_material_id, quantity) VALUES (?, ?, ?, ?)", orderID, item.ProductID, bom.RawMaterialID, usage); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}

	// 扣减原材料库存（允许负数）
	for rawMatID, deductQty := range rawMaterialDeductions {
		_, err = tx.Exec("UPDATE raw_materials SET stock = stock - ? WHERE id = ?", deductQty, rawMatID)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to deduct raw material ID %d: %v", rawMatID, err), http.StatusInternalServerError)
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
		"order_no": orderNo,
		"total":    total,
		"message":  "订单创建成功",
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

	query := `
        SELECT id, order_no, customer_name, region, customer_address, customer_phone,
               order_date, expected_shipping_date, delivery_date, total_amount, status, payment_status,
               prepared_by, payment_settlement, freight_payment, freight_recovery, transport_method,
               created_at, COALESCE(remark, '') AS remark
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
		err := rows.Scan(&o.ID, &o.OrderNo, &o.CustomerName, &o.Region, &o.CustomerAddress, &o.CustomerPhone,
			&orderDate, &expectedDate, &deliveryDate, &o.TotalAmount, &o.Status, &o.PaymentStatus,
			&o.PreparedBy, &o.PaymentSettlement, &o.FreightPayment, &o.FreightRecovery, &o.TransportMethod, &o.CreatedAt, &o.Remark)
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
               COALESCE(p.spec, '') AS spec, COALESCE(p.unit, '') AS unit
        FROM order_items oi
        JOIN products p ON oi.product_id = p.id
        WHERE oi.order_id = ?
    `, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []orderItemView
	for rows.Next() {
		var it orderItemView
		if err := rows.Scan(&it.ProductID, &it.Quantity, &it.Price, &it.ProductName, &it.Spec, &it.Unit); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
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
	err = models.DB.QueryRow(`
        SELECT id, order_no, customer_name, region, customer_address, customer_phone,
               order_date, expected_shipping_date, delivery_date, total_amount, status, payment_status,
               prepared_by, payment_settlement, freight_payment, freight_recovery, transport_method,
               created_at, COALESCE(remark, '') AS remark
        FROM orders WHERE id = ?
    `, id).Scan(&order.ID, &order.OrderNo, &order.CustomerName, &order.Region, &order.CustomerAddress, &order.CustomerPhone,
		&orderDate, &expectedDate, &deliveryDate, &order.TotalAmount, &order.Status, &order.PaymentStatus,
		&order.PreparedBy, &order.PaymentSettlement, &order.FreightPayment, &order.FreightRecovery, &order.TransportMethod, &order.CreatedAt, &order.Remark)
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

	// 已取消（status=4）的订单在取消时已归还过库存，删除时不再重复归还。
	if status != 4 {
		if err := restoreOrderStock(tx, id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
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

// restoreOrderStock 归还指定订单占用/扣减的成品库存，并按订单BOM快照归还对应原材料的库存。
// 供删除订单和取消订单共用；按快照归还可保证与下单时的扣减口径一致。
func restoreOrderStock(tx *sql.Tx, orderID int) error {
	items, err := loadOrderStockItems(tx, orderID)
	if err != nil {
		return err
	}

	// 归还成品库存。
	for _, it := range items {
		if _, err := tx.Exec("UPDATE products SET stock = stock + ? WHERE id = ?", it.Quantity, it.ProductID); err != nil {
			return fmt.Errorf("failed to restore product ID %d: %v", it.ProductID, err)
		}
	}

	// 归还该订单占用的原材料库存（按下单时BOM快照；历史订单无快照时按当前BOM）。
	rawMaterialReturns, err := orderRawMaterialUsage(tx, orderID, items)
	if err != nil {
		return err
	}
	for rawMatID, qty := range rawMaterialReturns {
		if _, err := tx.Exec("UPDATE raw_materials SET stock = stock + ? WHERE id = ?", qty, rawMatID); err != nil {
			return fmt.Errorf("failed to restore raw material ID %d: %v", rawMatID, err)
		}
	}
	return nil
}

// deductOrderStock 重新扣减指定订单的成品库存，并按订单BOM快照扣减对应原材料的库存。
// 与 restoreOrderStock 互逆：当已取消（status=4）的订单被重新置为有效状态
// （待生产/生产中/待发货/已发货）时调用，恢复“订单占用库存”，
// 扣减口径与创建订单时保持一致。
func deductOrderStock(tx *sql.Tx, orderID int) error {
	items, err := loadOrderStockItems(tx, orderID)
	if err != nil {
		return err
	}

	// 扣减成品库存（允许负数，与创建订单时一致）。
	for _, it := range items {
		if _, err := tx.Exec("UPDATE products SET stock = stock - ? WHERE id = ?", it.Quantity, it.ProductID); err != nil {
			return fmt.Errorf("failed to deduct product ID %d: %v", it.ProductID, err)
		}
	}

	// 扣减该订单占用的原材料库存（按下单时BOM快照；历史订单无快照时按当前BOM）。
	rawMaterialDeductions, err := orderRawMaterialUsage(tx, orderID, items)
	if err != nil {
		return err
	}
	for rawMatID, qty := range rawMaterialDeductions {
		if _, err := tx.Exec("UPDATE raw_materials SET stock = stock - ? WHERE id = ?", qty, rawMatID); err != nil {
			return fmt.Errorf("failed to deduct raw material ID %d: %v", rawMatID, err)
		}
	}
	return nil
}

// loadOrderStockItems 读取订单明细中需要调整库存的产品行。
func loadOrderStockItems(tx *sql.Tx, orderID int) ([]orderStockItem, error) {
	rows, err := tx.Query("SELECT product_id, quantity FROM order_items WHERE order_id = ?", orderID)
	if err != nil {
		return nil, fmt.Errorf("failed to load order items: %v", err)
	}
	defer rows.Close()

	var items []orderStockItem
	for rows.Next() {
		var it orderStockItem
		if err := rows.Scan(&it.ProductID, &it.Quantity); err != nil {
			return nil, fmt.Errorf("failed to read order item: %v", err)
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate order items: %v", err)
	}
	return items, nil
}

// orderRawMaterialUsage 汇总指定订单占用的原材料数量（按原材料ID）。
//
// 优先使用下单时写入的 BOM 快照（order_bom_snapshot）：即使之后产品 BOM 被修改，
// 取消/恢复/删除订单时归还或扣减的原材料数量仍与下单时一致；
// 历史订单没有快照时回退到当前 BOM（与原逻辑一致）。
func orderRawMaterialUsage(tx *sql.Tx, orderID int, items []orderStockItem) (map[int]float64, error) {
	usage := make(map[int]float64)

	snapshots, err := models.GetOrderBOMSnapshot(tx, orderID)
	if err != nil {
		return nil, err
	}
	if len(snapshots) > 0 {
		for _, s := range snapshots {
			usage[s.RawMaterialID] += s.Quantity
		}
		return usage, nil
	}

	// 历史订单无快照：按当前 BOM 计算。
	for _, it := range items {
		boms, err := models.GetBOMByProduct(it.ProductID)
		if err != nil {
			return nil, fmt.Errorf("failed to load BOM for product ID %d: %v", it.ProductID, err)
		}
		for _, bom := range boms {
			usage[bom.RawMaterialID] += bom.Quantity * float64(it.Quantity)
		}
	}
	return usage, nil
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
		Region               string `json:"region"`
		OrderDate            string `json:"order_date"`
		ExpectedShippingDate string `json:"expected_shipping_date"`
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

	var exists bool
	err = models.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM orders WHERE id = ?)", req.ID).Scan(&exists)
	if err != nil || !exists {
		http.Error(w, "Order not found", http.StatusNotFound)
		return
	}

	_, err = models.DB.Exec(`
        UPDATE orders SET
            customer_name = ?,
            region = ?,
            order_date = ?,
            expected_shipping_date = ?,
            payment_status = ?,
            prepared_by = ?,
            payment_settlement = ?,
            freight_payment = ?,
            freight_recovery = ?,
            transport_method = ?,
            remark = ?
        WHERE id = ?
    `, req.CustomerName, req.Region, orderDate, expectedDate, req.PaymentStatus, preparedBy, paymentSettlement, freightPayment, freightRecovery, transportMethod, req.Remark, req.ID)
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
	err = models.DB.QueryRow(`
        SELECT id, order_no, customer_name, region, customer_address, customer_phone,
               order_date, expected_shipping_date, delivery_date, total_amount, status, payment_status,
               prepared_by, payment_settlement, freight_payment, freight_recovery, transport_method,
               created_at, COALESCE(remark, '') AS remark
        FROM orders WHERE id = ?
    `, id).Scan(&order.ID, &order.OrderNo, &order.CustomerName, &order.Region, &order.CustomerAddress, &order.CustomerPhone,
		&orderDate, &expectedDate, &deliveryDate, &order.TotalAmount, &order.Status, &order.PaymentStatus,
		&order.PreparedBy, &order.PaymentSettlement, &order.FreightPayment, &order.FreightRecovery, &order.TransportMethod, &order.CreatedAt, &order.Remark)
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

	data := struct {
		Order         models.Order
		Items         []Item
		Now           time.Time
		TotalQuantity int
	}{
		Order:         order,
		Items:         items,
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
