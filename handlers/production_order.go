package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"order-system/models"
	"time"
)

// productionOrderItemView 描述订单中可用于生产入库的产品明细，不暴露订单价格。
type productionOrderItemView struct {
	ProductID         int     `json:"product_id"`
	ProductName       string  `json:"product_name"`
	Spec              string  `json:"spec"`
	Unit              string  `json:"unit"`
	Quantity          int     `json:"quantity"`
	ShippedQuantity   float64 `json:"shipped_quantity"`
	RemainingQuantity float64 `json:"remaining_quantity"`
}

// productionOrderView 是生产入库时可按订单带入的订单及未发货明细。
type productionOrderView struct {
	ID           int                       `json:"id"`
	OrderNo      string                    `json:"order_no"`
	CustomerName string                    `json:"customer_name"`
	OrderDate    *time.Time                `json:"order_date"`
	Status       int                       `json:"status"`
	Items        []productionOrderItemView `json:"items"`
}

// ListProductionOrders 返回可用于生产入库的订单选项。
// 仅返回待生产、生产中、待发货且仍有未发货数量的订单。
func ListProductionOrders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rows, err := models.DB.Query(`
		SELECT o.id, o.order_no, o.customer_name, o.order_date, o.status,
		       oi.product_id, p.name, COALESCE(p.spec, ''), COALESCE(p.unit, ''),
		       oi.quantity, COALESCE(SUM(poi.quantity), 0) AS shipped_quantity
		FROM orders o
		JOIN order_items oi ON oi.order_id = o.id
		JOIN products p ON p.id = oi.product_id
		LEFT JOIN product_outbound_items poi ON poi.order_item_id = oi.id
		WHERE o.status IN (0, 1, 2)
		GROUP BY o.id, o.order_no, o.customer_name, o.order_date, o.status,
		         oi.id, oi.product_id, p.name, p.spec, p.unit, oi.quantity
		ORDER BY o.id DESC, oi.id ASC
	`)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	result := make([]productionOrderView, 0)
	indexByOrder := make(map[int]int)
	for rows.Next() {
		var orderID, status int
		var orderNo, customerName string
		var orderDate sql.NullTime
		var item productionOrderItemView
		if err := rows.Scan(
			&orderID, &orderNo, &customerName, &orderDate, &status,
			&item.ProductID, &item.ProductName, &item.Spec, &item.Unit,
			&item.Quantity, &item.ShippedQuantity,
		); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}

		index, exists := indexByOrder[orderID]
		if !exists {
			index = len(result)
			indexByOrder[orderID] = index
			result = append(result, productionOrderView{
				ID:           orderID,
				OrderNo:      orderNo,
				CustomerName: customerName,
				Status:       status,
			})
			if orderDate.Valid {
				result[index].OrderDate = &orderDate.Time
			}
		}

		item.RemainingQuantity = float64(item.Quantity) - item.ShippedQuantity
		if item.RemainingQuantity < 0 {
			item.RemainingQuantity = 0
		}
		if item.RemainingQuantity > 0.0005 {
			result[index].Items = append(result[index].Items, item)
		}
	}
	if err := rows.Err(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	filtered := result[:0]
	for _, order := range result {
		if len(order.Items) > 0 {
			filtered = append(filtered, order)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(filtered)
}
