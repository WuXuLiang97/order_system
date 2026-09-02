package handlers

import (
	"encoding/json"
	"net/http"
	"order-system/models"
	"strconv"
	"strings"
)

// ListUsers 获取用户列表（管理员）
func ListUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	users, err := models.GetAllUsers()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(users)
}

// AddUser 添加用户（管理员）
func AddUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
		Role        string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	if req.Username == "" {
		writeJSONError(w, http.StatusBadRequest, "用户名不能为空")
		return
	}
	if req.Password == "" {
		writeJSONError(w, http.StatusBadRequest, "密码不能为空")
		return
	}
	if !models.IsValidRole(req.Role) {
		writeJSONError(w, http.StatusBadRequest, "角色不合法，仅支持 admin（管理员）或 user（普通用户）")
		return
	}

	// 检查用户名是否已存在
	if _, err := models.GetUserByUsername(req.Username); err == nil {
		writeJSONError(w, http.StatusBadRequest, "用户名已存在")
		return
	} else if err != models.ErrUserNotFound {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	hash, err := models.HashPassword(req.Password)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "密码加密失败")
		return
	}
	id, err := models.CreateUser(req.Username, hash, req.DisplayName, req.Role)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":      id,
		"message": "用户添加成功",
	})
}

// DeleteUser 删除用户（管理员，不能删除自己）
func DeleteUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	current := CurrentUser(r)
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		writeJSONError(w, http.StatusBadRequest, "缺少 id 参数")
		return
	}
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		writeJSONError(w, http.StatusBadRequest, "无效的 id")
		return
	}
	if current != nil && current.ID == id {
		writeJSONError(w, http.StatusBadRequest, "不能删除当前登录的账号")
		return
	}
	if err := models.DeleteUser(id); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "用户删除成功"})
}

// ResetPassword 重置用户密码（管理员）
func ResetPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		ID          int    `json:"id"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if req.ID <= 0 {
		writeJSONError(w, http.StatusBadRequest, "无效的用户ID")
		return
	}
	if req.NewPassword == "" {
		writeJSONError(w, http.StatusBadRequest, "新密码不能为空")
		return
	}
	if _, err := models.GetUserByID(req.ID); err == models.ErrUserNotFound {
		writeJSONError(w, http.StatusNotFound, "用户不存在")
		return
	} else if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	hash, err := models.HashPassword(req.NewPassword)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "密码加密失败")
		return
	}
	if err := models.UpdateUserPassword(req.ID, hash); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "密码重置成功"})
}