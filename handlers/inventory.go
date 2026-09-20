package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"order-system/models"
)

// GetPurchaseDemand 返回原材料实际库存、占用、在途、净需求和建议采购量。
func GetPurchaseDemand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	list, err := models.GetPurchaseDemand()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// ListStockMovements 查询不可变库存与成本流水。
func ListStockMovements(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit := 100
	if value, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && value > 0 {
		limit = value
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	itemID, _ := strconv.Atoi(r.URL.Query().Get("item_id"))
	filter := models.StockMovementFilter{
		StartDate:    strings.TrimSpace(r.URL.Query().Get("start_date")),
		EndDate:      strings.TrimSpace(r.URL.Query().Get("end_date")),
		ItemType:     strings.TrimSpace(r.URL.Query().Get("item_type")),
		ItemID:       itemID,
		Direction:    strings.TrimSpace(r.URL.Query().Get("direction")),
		MovementType: strings.TrimSpace(r.URL.Query().Get("movement_type")),
		ReferenceNo:  strings.TrimSpace(r.URL.Query().Get("reference_no")),
		Operator:     strings.TrimSpace(r.URL.Query().Get("operator")),
		Limit:        limit,
		Offset:       offset,
	}
	list, err := models.ListStockMovementsFiltered(filter)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}
