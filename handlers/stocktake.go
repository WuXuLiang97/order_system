package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"order-system/models"
)

func stocktakeCanManageScope(r *http.Request, scope string) bool {
	user := CurrentUser(r)
	if user == nil {
		return false
	}
	switch scope {
	case models.StocktakeScopeAll:
		return HasPermission(user, PermProductManage) && HasPermission(user, PermMaterialManage)
	case models.StocktakeScopeProduct:
		return HasPermission(user, PermProductManage)
	case models.StocktakeScopeRawMaterial:
		return HasPermission(user, PermMaterialManage)
	default:
		return false
	}
}

func stocktakeViewAccess(r *http.Request) (product, rawMaterial bool) {
	user := CurrentUser(r)
	if user == nil {
		return false, false
	}
	perms := PermissionsFor(user)
	product = perms[PermProductView] || perms[PermProductManage]
	rawMaterial = perms[PermMaterialView] || perms[PermMaterialManage]
	return product, rawMaterial
}

func stocktakeCanViewScope(r *http.Request, scope string) bool {
	product, rawMaterial := stocktakeViewAccess(r)
	switch scope {
	case models.StocktakeScopeAll:
		return product && rawMaterial
	case models.StocktakeScopeProduct:
		return product
	case models.StocktakeScopeRawMaterial:
		return rawMaterial
	default:
		return false
	}
}

func ListStocktakes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	product, rawMaterial := stocktakeViewAccess(r)
	list, err := models.ListStocktakes(product, rawMaterial)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

func GetStocktake(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if err != nil || id <= 0 {
		writeJSONError(w, http.StatusBadRequest, "盘点单ID无效")
		return
	}
	order, err := models.GetStocktake(id)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "盘点单不存在")
		return
	}
	if !stocktakeCanViewScope(r, order.ScopeType) {
		writeJSONError(w, http.StatusForbidden, "没有该盘点范围的库存查看权限")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(order)
}

func CreateStocktake(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		StocktakeDate string `json:"stocktake_date"`
		ScopeType     string `json:"scope_type"`
		Remark        string `json:"remark"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	scope := strings.TrimSpace(req.ScopeType)
	if !stocktakeCanManageScope(r, scope) {
		writeJSONError(w, http.StatusForbidden, "没有该盘点范围的库存管理权限")
		return
	}
	stocktakeDate, err := businessTimeFromDate(req.StocktakeDate)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	userID := 0
	if user := CurrentUser(r); user != nil {
		userID = user.ID
	}
	order, err := models.CreateStocktake(stocktakeDate, scope, strings.TrimSpace(req.Remark), userID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(order)
}

func UpdateStocktake(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID            int64                       `json:"id"`
		StocktakeDate string                      `json:"stocktake_date"`
		Remark        string                      `json:"remark"`
		Items         []models.StocktakeLineInput `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.ID <= 0 {
		writeJSONError(w, http.StatusBadRequest, "盘点单ID无效")
		return
	}
	order, err := models.GetStocktake(req.ID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "盘点单不存在")
		return
	}
	if !stocktakeCanManageScope(r, order.ScopeType) {
		writeJSONError(w, http.StatusForbidden, "没有该盘点范围的库存管理权限")
		return
	}
	stocktakeDate, err := businessTimeFromDate(req.StocktakeDate)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	userID := 0
	if user := CurrentUser(r); user != nil {
		userID = user.ID
	}
	updated, err := models.UpdateStocktakeDraft(req.ID, stocktakeDate, strings.TrimSpace(req.Remark), req.Items, userID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updated)
}

func submitOrPostStocktake(w http.ResponseWriter, r *http.Request, post bool) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.ID <= 0 {
		writeJSONError(w, http.StatusBadRequest, "盘点单ID无效")
		return
	}
	order, err := models.GetStocktake(req.ID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "盘点单不存在")
		return
	}
	if !stocktakeCanManageScope(r, order.ScopeType) {
		writeJSONError(w, http.StatusForbidden, "没有该盘点范围的库存管理权限")
		return
	}
	userID := 0
	if user := CurrentUser(r); user != nil {
		userID = user.ID
	}
	var result *models.StocktakeOrder
	if post {
		result, err = models.PostStocktake(req.ID, userID)
	} else {
		result, err = models.SubmitStocktake(req.ID, userID)
	}
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func SubmitStocktake(w http.ResponseWriter, r *http.Request) {
	submitOrPostStocktake(w, r, false)
}

func PostStocktake(w http.ResponseWriter, r *http.Request) {
	submitOrPostStocktake(w, r, true)
}

func CancelStocktake(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.ID <= 0 {
		writeJSONError(w, http.StatusBadRequest, "盘点单ID无效")
		return
	}
	order, err := models.GetStocktake(req.ID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "盘点单不存在")
		return
	}
	if !stocktakeCanManageScope(r, order.ScopeType) {
		writeJSONError(w, http.StatusForbidden, "没有该盘点范围的库存管理权限")
		return
	}
	userID := 0
	if user := CurrentUser(r); user != nil {
		userID = user.ID
	}
	updated, err := models.CancelStocktake(req.ID, userID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updated)
}
