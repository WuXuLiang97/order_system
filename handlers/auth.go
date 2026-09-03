package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"order-system/models"
	"strings"
	"sync"
	"time"
)

// ============================================
// 权限定义与判断：
//   - admin 角色拥有全部权限；
//   - 普通用户按 user_permissions 表按用户授权。
//
// 库存/原材料/采购/客户管理：查看 / 编辑（编辑=manage，隐含查看）。
// 订单管理：按“查看范围 × 编辑范围”授权：
//
//	order:view_own   查看自己创建的
//	order:view_all   查看全部
//	order:edit_own   编辑自己创建的（含新建/修改/状态流转；不含删除）
//	order:edit_all   编辑全部（含删除），隐含 view_all
//
// ============================================
const (
	PermOrderViewOwn   = "order:view_own"
	PermOrderViewAll   = "order:view_all"
	PermOrderEditOwn   = "order:edit_own"
	PermOrderEditAll   = "order:edit_all"
	PermProductView    = "product:view"
	PermProductManage  = "product:manage"
	PermMaterialView   = "material:view"
	PermMaterialManage = "material:manage"
	PermPurchaseView   = "purchase:view"
	PermPurchaseManage = "purchase:manage"
	PermCustomerView   = "customer:view"
	PermCustomerManage = "customer:manage"
	PermUserManage     = "user:manage"
)

// allPermissions 系统全部权限（admin 拥有，普通用户按授权表）。
var allPermissions = []string{
	PermOrderViewOwn, PermOrderViewAll, PermOrderEditOwn, PermOrderEditAll,
	PermProductView, PermProductManage,
	PermMaterialView, PermMaterialManage,
	PermPurchaseView, PermPurchaseManage,
	PermCustomerView, PermCustomerManage,
	PermUserManage,
}

// grantableGroups 用户管理中可授予的权限分组，组内非首权限会自动补上首权限（编辑隐含查看）。
var grantableGroups = [][]string{
	{PermOrderViewOwn, PermOrderEditOwn},
	{PermOrderViewAll, PermOrderEditAll},
	{PermProductView, PermProductManage},
	{PermMaterialView, PermMaterialManage},
	{PermPurchaseView, PermPurchaseManage},
	{PermCustomerView, PermCustomerManage},
}

// NormalizeGrantedPermissions 校验并规整授权列表：只允许授予 grantableGroups 内的权限；
// 组内存在任意非“查看”权限时自动补上该组“查看”权限（编辑隐含查看）。返回顺序稳定。
func NormalizeGrantedPermissions(perms []string) ([]string, error) {
	allowed := map[string]bool{}
	for _, g := range grantableGroups {
		for _, p := range g {
			allowed[p] = true
		}
	}
	set := map[string]bool{}
	for _, p := range perms {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !allowed[p] {
			return nil, fmt.Errorf("不允许的权限: %s", p)
		}
		set[p] = true
	}
	for _, g := range grantableGroups {
		for _, p := range g[1:] {
			if set[p] {
				set[g[0]] = true
				break
			}
		}
	}
	var result []string
	for _, g := range grantableGroups {
		for _, p := range g {
			if set[p] {
				result = append(result, p)
			}
		}
	}
	return result, nil
}

// PermissionsFor 返回用户拥有的权限集合
func PermissionsFor(u *models.User) map[string]bool {
	perms := map[string]bool{}
	if u == nil {
		return perms
	}
	if u.Role == models.RoleAdmin {
		for _, p := range allPermissions {
			perms[p] = true
		}
		return perms
	}
	if ps, err := models.GetUserPermissions(u.ID); err == nil {
		for _, p := range ps {
			perms[p] = true
		}
	}
	return perms
}

// HasPermission 判断用户是否拥有指定权限
func HasPermission(u *models.User, perm string) bool {
	if u == nil {
		return false
	}
	return PermissionsFor(u)[perm]
}

// isOwnOrder 判断用户是否为订单的创建人或负责人。
func isOwnOrder(u *models.User, createdByID, ownerUserID *int) bool {
	if u == nil {
		return false
	}
	if createdByID != nil && *createdByID == u.ID {
		return true
	}
	return ownerUserID != nil && *ownerUserID == u.ID
}

// canViewOrder 判断用户能否查看指定订单（查看/编辑全部=true；“仅本人”=创建人或负责人）。
func canViewOrder(u *models.User, createdByID, ownerUserID *int) bool {
	if u == nil {
		return false
	}
	perms := PermissionsFor(u)
	if perms[PermOrderViewAll] || perms[PermOrderEditAll] {
		return true
	}
	if perms[PermOrderViewOwn] || perms[PermOrderEditOwn] {
		return isOwnOrder(u, createdByID, ownerUserID)
	}
	return false
}

// canEditOrder 判断用户能否编辑指定订单（编辑全部=true；“仅本人”=创建人或负责人，且不含删除）。
func canEditOrder(u *models.User, createdByID, ownerUserID *int) bool {
	if u == nil {
		return false
	}
	perms := PermissionsFor(u)
	if perms[PermOrderEditAll] {
		return true
	}
	if perms[PermOrderEditOwn] {
		return isOwnOrder(u, createdByID, ownerUserID)
	}
	return false
}

// ============================================
// 会话管理（内存存储）
// ============================================
const sessionCookieName = "order_session"

type session struct {
	userID  int
	expires time.Time
}

var (
	sessionStore  = make(map[string]*session)
	sessionMu     sync.Mutex
	sessionMaxAge = 12 * time.Hour
)

func createSession(userID int) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)
	sessionMu.Lock()
	defer sessionMu.Unlock()
	sessionStore[token] = &session{userID: userID, expires: time.Now().Add(sessionMaxAge)}
	return token, nil
}

func getSession(r *http.Request) *session {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return nil
	}
	sessionMu.Lock()
	defer sessionMu.Unlock()
	s, ok := sessionStore[cookie.Value]
	if !ok {
		return nil
	}
	if time.Now().After(s.expires) {
		delete(sessionStore, cookie.Value)
		return nil
	}
	return s
}

func destroySession(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		sessionMu.Lock()
		delete(sessionStore, cookie.Value)
		sessionMu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// CurrentUser 返回当前登录用户；未登录返回 nil
func CurrentUser(r *http.Request) *models.User {
	s := getSession(r)
	if s == nil {
		return nil
	}
	u, err := models.GetUserByID(s.userID)
	if err != nil {
		return nil
	}
	return u
}

// ============================================
// 鉴权中间件
// ============================================
func isAPIRequest(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, "/api/")
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// RequireAuth 要求登录；页面跳转 /login，API 返回 401
func RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if CurrentUser(r) == nil {
			if isAPIRequest(r) {
				writeJSONError(w, http.StatusUnauthorized, "未登录或会话已过期，请先登录")
			} else {
				http.Redirect(w, r, "/login", http.StatusFound)
			}
			return
		}
		next(w, r)
	}
}

// RequirePermission 要求登录并拥有指定权限；页面返回 403，API 返回 403 JSON
func RequirePermission(perm string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := CurrentUser(r)
		if user == nil {
			if isAPIRequest(r) {
				writeJSONError(w, http.StatusUnauthorized, "未登录或会话已过期，请先登录")
			} else {
				http.Redirect(w, r, "/login", http.StatusFound)
			}
			return
		}
		if !HasPermission(user, perm) {
			if isAPIRequest(r) {
				writeJSONError(w, http.StatusForbidden, "没有权限执行该操作")
			} else {
				http.Error(w, "没有权限访问该页面", http.StatusForbidden)
			}
			return
		}
		next(w, r)
	}
}

// RequireAnyPermission 要求登录且拥有所列权限中的任意一个（用于跨模块共享引用的只读接口）。
func RequireAnyPermission(perms ...string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			user := CurrentUser(r)
			if user == nil {
				if isAPIRequest(r) {
					writeJSONError(w, http.StatusUnauthorized, "未登录或会话已过期，请先登录")
				} else {
					http.Redirect(w, r, "/login", http.StatusFound)
				}
				return
			}
			ok := false
			for _, p := range perms {
				if HasPermission(user, p) {
					ok = true
					break
				}
			}
			if !ok {
				if isAPIRequest(r) {
					writeJSONError(w, http.StatusForbidden, "没有权限执行该操作")
				} else {
					http.Error(w, "没有权限访问该页面", http.StatusForbidden)
				}
				return
			}
			next(w, r)
		}
	}
}

// ============================================
// 页面数据：模板中可用 .User / .Can / .PermsJSON / .UserJSON
// ============================================
type PageData struct {
	User      *models.User
	UserJSON  template.JS
	Perms     map[string]bool
	PermsJSON template.JS
}

// Can 模板中判断当前用户是否拥有某权限
func (p PageData) Can(perm string) bool {
	if p.Perms == nil {
		return false
	}
	return p.Perms[perm]
}

// CanAny 判断当前用户是否拥有所列权限中的任意一个。
func (p PageData) CanAny(perms ...string) bool {
	if p.Perms == nil {
		return false
	}
	for _, perm := range perms {
		if p.Perms[perm] {
			return true
		}
	}
	return false
}

// PageDataFor 根据当前请求构造页面数据
func PageDataFor(r *http.Request) PageData {
	user := CurrentUser(r)
	perms := PermissionsFor(user)

	var userJSON template.JS = "null"
	if user != nil {
		payload := map[string]interface{}{
			"id":           user.ID,
			"username":     user.Username,
			"display_name": user.DisplayName,
			"role":         user.Role,
			"is_admin":     user.IsAdmin(),
		}
		if b, err := json.Marshal(payload); err == nil {
			userJSON = template.JS(b)
		}
	}

	permBytes, _ := json.Marshal(perms)
	return PageData{
		User:      user,
		UserJSON:  userJSON,
		Perms:     perms,
		PermsJSON: template.JS(permBytes),
	}
}

// RenderPage 渲染带布局的页面并注入用户数据
func RenderPage(w http.ResponseWriter, r *http.Request, files ...string) {
	tmpl, err := template.ParseFiles(files...)
	if err != nil {
		http.Error(w, "Template parse error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := tmpl.Execute(w, PageDataFor(r)); err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
	}
}

// ============================================
// 登录 / 登出 / 修改密码 / 当前用户信息
// ============================================
func LoginPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if CurrentUser(r) != nil {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	tmpl, err := template.ParseFiles("templates/login.html")
	if err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
		return
	}
	tmpl.Execute(w, nil)
}

func Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		writeJSONError(w, http.StatusBadRequest, "请输入用户名和密码")
		return
	}

	user, err := models.GetUserByUsername(req.Username)
	if err != nil || user == nil || !models.CheckPassword(req.Password, user.PasswordHash) {
		writeJSONError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}

	token, err := createSession(user.ID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "登录失败，请稍后重试")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionMaxAge.Seconds()),
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "登录成功",
		"user":    user,
	})
}

func Logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	destroySession(w, r)
	if r.Method == http.MethodGet {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "已退出登录"})
}

func ChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	user := CurrentUser(r)
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "未登录或会话已过期")
		return
	}

	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if req.NewPassword == "" {
		writeJSONError(w, http.StatusBadRequest, "新密码不能为空")
		return
	}
	if !models.CheckPassword(req.OldPassword, user.PasswordHash) {
		writeJSONError(w, http.StatusBadRequest, "原密码错误")
		return
	}

	newHash, err := models.HashPassword(req.NewPassword)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "密码加密失败")
		return
	}
	if err := models.UpdateUserPassword(user.ID, newHash); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "修改密码失败")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "密码修改成功"})
}

// ChangeDisplayName 修改当前登录用户的显示名称（用户可修改自己的名字）
func ChangeDisplayName(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	user := CurrentUser(r)
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "未登录或会话已过期")
		return
	}

	var req struct {
		DisplayName string `json:"display_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	if req.DisplayName == "" {
		writeJSONError(w, http.StatusBadRequest, "显示名称不能为空")
		return
	}
	if len([]rune(req.DisplayName)) > 100 {
		writeJSONError(w, http.StatusBadRequest, "显示名称不能超过100个字符")
		return
	}

	if err := models.UpdateUserDisplayName(user.ID, req.DisplayName); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "修改名称失败")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "名称修改成功"})
}

func CurrentUserInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	user := CurrentUser(r)
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "未登录或会话已过期")
		return
	}
	payload := map[string]interface{}{
		"id":           user.ID,
		"username":     user.Username,
		"display_name": user.DisplayName,
		"role":         user.Role,
		"is_admin":     user.IsAdmin(),
		"permissions":  PermissionsFor(user),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(payload)
}
