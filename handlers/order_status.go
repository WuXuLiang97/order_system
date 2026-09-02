package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"order-system/models"
)

// UpdateOrderStatus 更新订单状态，并按状态流转自动调整库存。
//
// 库存占用原则：订单创建（status=0 待生产）时即扣减成品与原材料库存；
// 只要订单处于有效状态（0 待生产 / 1 生产中 / 2 待发货 / 3 已发货），库存保持被占用。
//
// 状态流转对库存的影响：
//   - 有效状态 -> 取消（status=4）：归还成品与原材料库存（取消已归还过，删除时不再归还）。
//   - 取消(4) -> 有效状态（例如改为待发货）：重新扣减成品与原材料库存。
//   - 其它流转（有效状态之间、取消->取消）：库存不变。
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

	// 锁定订单行并读取当前状态；在同一事务内根据“当前状态 -> 目标状态”决定库存调整，
	// 可避免并发取消/恢复时库存被重复归还或重复扣减。
	var currentStatus int
	var ownerID sql.NullInt64
	err = tx.QueryRow("SELECT status, created_by_user_id FROM orders WHERE id = ? FOR UPDATE", req.ID).Scan(&currentStatus, &ownerID)
	if err == sql.ErrNoRows {
		http.Error(w, "Order not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !canEditOrder(CurrentUser(r), nullIntPtr(ownerID)) {
		writeJSONError(w, http.StatusForbidden, "没有权限修改该订单状态")
		return
	}

	// 1) 取消订单（且此前不是取消状态）：归还成品与原材料库存。
	if req.Status == 4 && currentStatus != 4 {
		if err := restoreOrderStock(tx, req.ID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// 2) 已取消订单恢复为有效状态（例如改为待发货）：重新扣减成品与原材料库存，
	//    否则取消时归还的库存会一直“空放着”，在途订单将不再占用库存。
	if req.Status != 4 && currentStatus == 4 {
		if err := deductOrderStock(tx, req.ID); err != nil {
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
