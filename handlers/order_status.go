package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"order-system/models"
)

// UpdateOrderStatus 更新订单状态。
// 当订单被取消（status=4）时，自动归还创建订单时扣减的成品库存和原材料库存。
func UpdateOrderStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ID     int `json:"id"`
		Status int `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.ID <= 0 || req.Status < 0 || req.Status > 4 {
		http.Error(w, "Invalid id or status", http.StatusBadRequest)
		return
	}

	tx, err := models.DB.Begin()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// 锁定订单行并读取当前状态，避免并发取消时重复归还库存。
	var currentStatus int
	err = tx.QueryRow("SELECT status FROM orders WHERE id = ? FOR UPDATE", req.ID).Scan(&currentStatus)
	if err == sql.ErrNoRows {
		http.Error(w, "Order not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 取消订单且订单此前不是已取消状态时，归还成品和原材料库存。
	if req.Status == 4 && currentStatus != 4 {
		if err := restoreOrderStock(tx, req.ID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	_, err = tx.Exec("UPDATE orders SET status = ? WHERE id = ?", req.Status, req.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Status updated successfully"})
}
