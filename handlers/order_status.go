package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"order-system/models"
)

// UpdateOrderStatus 更新订单状态，并按状态流转调整订单占用；从不修改实际库存。
//
// 占用原则：下单时只占用当前可用成品库存，不足部分进入生产和采购净需求；
// 只要订单有效，占用保持；实际库存只在生产、领料、采购到货和出库时变化。
//
// 状态流转对库存的影响：
//   - 有效状态 -> 取消（status=4）：释放尚未出库的成品占用。
//   - 取消(4) -> 有效状态：按当前可用库存重新建立成品占用。
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
	var orderOwner sql.NullInt64
	err = tx.QueryRow("SELECT status, created_by_user_id, owner_user_id FROM orders WHERE id = ? FOR UPDATE", req.ID).Scan(&currentStatus, &ownerID, &orderOwner)
	if err == sql.ErrNoRows {
		http.Error(w, "Order not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !canEditOrder(CurrentUser(r), nullIntPtr(ownerID), nullIntPtr(orderOwner)) {
		writeJSONError(w, http.StatusForbidden, "没有权限修改该订单状态")
		return
	}

	// 有关联送货单的订单不能取消，避免已发货库存与订单状态、库存回补相互冲突。
	if req.Status == 4 && currentStatus != 4 {
		var outboundCount int
		if err := tx.QueryRow("SELECT COUNT(*) FROM product_outbound WHERE order_id = ?", req.ID).Scan(&outboundCount); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if outboundCount > 0 {
			writeJSONError(w, http.StatusBadRequest, "该订单已有关联送货单，请先删除送货单后再取消")
			return
		}
	}

	// 1) 取消订单（且此前不是取消状态）：释放尚未出库的成品占用。
	if req.Status == 4 && currentStatus != 4 {
		if err := restoreOrderStock(tx, req.ID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// 2) 已取消订单恢复为有效状态：按当前可用成品库存重新建立占用，
	//    保证恢复后的订单仍参与可用库存与净需求计算。
	if req.Status != 4 && currentStatus == 4 {
		if err := deductOrderStock(tx, req.ID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// 状态流转为“已发货”时记录实际发货日期（首次发货时写入，重复流转不覆盖）。
	if req.Status == 3 {
		_, err = tx.Exec("UPDATE orders SET status = 3, shipped_date = COALESCE(shipped_date, CURDATE()) WHERE id = ?", req.ID)
	} else {
		_, err = tx.Exec("UPDATE orders SET status = ? WHERE id = ?", req.Status, req.ID)
	}
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
