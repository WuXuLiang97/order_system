package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
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
		type orderItem struct {
			ProductID int
			Quantity  int
		}

		rows, err := tx.Query("SELECT product_id, quantity FROM order_items WHERE order_id = ?", req.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var items []orderItem
		for rows.Next() {
			var it orderItem
			if err := rows.Scan(&it.ProductID, &it.Quantity); err != nil {
				rows.Close()
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			items = append(items, it)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		rawMaterialReturns := make(map[int]float64)
		for _, it := range items {
			// 归还成品库存。
			_, err = tx.Exec("UPDATE products SET stock = stock + ? WHERE id = ?", it.Quantity, it.ProductID)
			if err != nil {
				http.Error(w, fmt.Sprintf("Failed to restore product ID %d: %v", it.ProductID, err), http.StatusInternalServerError)
				return
			}

			// 归还该产品对应 BOM 的原材料库存。
			boms, err := models.GetBOMByProduct(it.ProductID)
			if err != nil {
				http.Error(w, fmt.Sprintf("Failed to load BOM for product ID %d: %v", it.ProductID, err), http.StatusInternalServerError)
				return
			}
			for _, bom := range boms {
				rawMaterialReturns[bom.RawMaterialID] += bom.Quantity * float64(it.Quantity)
			}
		}

		for rawMatID, qty := range rawMaterialReturns {
			_, err = tx.Exec("UPDATE raw_materials SET stock = stock + ? WHERE id = ?", qty, rawMatID)
			if err != nil {
				http.Error(w, fmt.Sprintf("Failed to restore raw material ID %d: %v", rawMatID, err), http.StatusInternalServerError)
				return
			}
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
