package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"order-system/models"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// 客户默认值
const (
	defaultCustomerStatus = "潜在客户"
	defaultCurrency       = "CNY"
	defaultRiskLevel      = "低"
)

type customerReq struct {
	ID               int      `json:"id"`
	Name             string   `json:"name"`
	Phone            string   `json:"phone"`
	Address          string   `json:"address"`
	Code             string   `json:"code"`
	FullName         string   `json:"full_name"`
	CustomerType     string   `json:"customer_type"`
	Status           string   `json:"status"`
	Level            string   `json:"level"`
	CooperationModes []string `json:"cooperation_modes"`
	BrandNames       []string `json:"brand_names"`
	TargetMarkets    []string `json:"target_markets"`
	Currency         string   `json:"currency"`
	PaymentTerms     string   `json:"payment_terms"`
	TradeTerms       string   `json:"trade_terms"`
	CountryRegion    string   `json:"country_region"`
	InvoiceInfo      string   `json:"invoice_info"`
	ShippingMark     string   `json:"shipping_mark"`
	Source           string   `json:"source"`
	OwnerUserID      int      `json:"owner_user_id"`
	RiskLevel        string   `json:"risk_level"`
	Remark           string   `json:"remark"`
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// ============ 客户数据权限（行级隔离） ============

// isPublicCustomer 历史公共客户：无负责人且无创建人（数据隔离启用前录入）。
func isPublicCustomer(c *models.Customer) bool {
	return c.OwnerUserID == 0 && c.CreatedByUserID == 0
}

// canViewCustomer 是否可查看该客户：管理员全部；普通用户仅本人负责 / 本人创建 / 历史公共客户。
func canViewCustomer(user *models.User, c *models.Customer) bool {
	if user == nil {
		return false
	}
	if user.IsAdmin() {
		return true
	}
	return isPublicCustomer(c) || c.OwnerUserID == user.ID || c.CreatedByUserID == user.ID
}

// canManageCustomer 是否可编辑 / 删除该客户（规则同 canViewCustomer）。
func canManageCustomer(user *models.User, c *models.Customer) bool {
	if user == nil {
		return false
	}
	if user.IsAdmin() {
		return true
	}
	return isPublicCustomer(c) || c.OwnerUserID == user.ID || c.CreatedByUserID == user.ID
}

// customerAttachmentCategories 客户附件分类
var customerAttachmentCategories = []string{"OEM协议", "保密协议", "标签样稿", "合同扫描件", "唛头", "其他"}

// customerAttachmentExts 允许上传的附件扩展名
var customerAttachmentExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true,
	".pdf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true,
	".ppt": true, ".pptx": true, ".txt": true, ".zip": true, ".rar": true,
}

// isValidCustomerCode 校验客户编号：KHCN/KHFW + 数字流水号。
func isValidCustomerCode(code string) bool {
	if !strings.HasPrefix(code, "KHCN") && !strings.HasPrefix(code, "KHFW") {
		return false
	}
	numStr := strings.TrimPrefix(strings.TrimPrefix(code, "KHCN"), "KHFW")
	if numStr == "" {
		return false
	}
	for _, ch := range numStr {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func cleanCSVList(items []string) []string {
	var out []string
	for _, it := range items {
		it = strings.TrimSpace(it)
		if it != "" {
			out = append(out, it)
		}
	}
	return out
}

// normalizeCustomerReq 填充默认值并清理多选字段。
func normalizeCustomerReq(req *customerReq) {
	req.Name = strings.TrimSpace(req.Name)
	req.Phone = strings.TrimSpace(req.Phone)
	req.Address = strings.TrimSpace(req.Address)
	req.Code = strings.TrimSpace(req.Code)
	req.FullName = strings.TrimSpace(req.FullName)
	req.CountryRegion = strings.TrimSpace(req.CountryRegion)
	req.InvoiceInfo = strings.TrimSpace(req.InvoiceInfo)
	req.ShippingMark = strings.TrimSpace(req.ShippingMark)
	req.Remark = strings.TrimSpace(req.Remark)
	req.CooperationModes = cleanCSVList(req.CooperationModes)
	req.BrandNames = cleanCSVList(req.BrandNames)
	req.TargetMarkets = cleanCSVList(req.TargetMarkets)
	if req.Status == "" {
		req.Status = defaultCustomerStatus
	}
	if req.Currency == "" {
		req.Currency = defaultCurrency
	}
	if req.RiskLevel == "" {
		req.RiskLevel = defaultRiskLevel
	}
}

// validateCustomerReq 校验客户必填与联动规则。
func validateCustomerReq(req *customerReq) error {
	if req.Name == "" {
		return errors.New("客户简称（名称）不能为空")
	}
	if req.Code != "" && !isValidCustomerCode(req.Code) {
		return errors.New("客户编号格式不正确，应为 KHCN/KHFW 加数字流水号")
	}
	if req.CustomerType != "" && !containsStr(models.CustomerTypes, req.CustomerType) {
		return errors.New("客户类型不在可选范围内")
	}
	if models.IsOEMCustomerType(req.CustomerType) && len(req.BrandNames) == 0 {
		return errors.New("OEM 客户必须填写客户品牌名")
	}
	if models.IsForeignCustomerType(req.CustomerType) && req.CountryRegion == "" {
		return errors.New("境外客户必须填写国家/地区")
	}
	return nil
}

func customerFromReq(req *customerReq) models.Customer {
	return models.Customer{
		ID:               req.ID,
		Name:             req.Name,
		Phone:            req.Phone,
		Address:          req.Address,
		Code:             req.Code,
		FullName:         req.FullName,
		CustomerType:     req.CustomerType,
		Status:           req.Status,
		Level:            req.Level,
		CooperationModes: req.CooperationModes,
		BrandNames:       req.BrandNames,
		TargetMarkets:    req.TargetMarkets,
		Currency:         req.Currency,
		PaymentTerms:     req.PaymentTerms,
		TradeTerms:       req.TradeTerms,
		CountryRegion:    req.CountryRegion,
		InvoiceInfo:      req.InvoiceInfo,
		ShippingMark:     req.ShippingMark,
		Source:           req.Source,
		OwnerUserID:      req.OwnerUserID,
		RiskLevel:        req.RiskLevel,
		Remark:           req.Remark,
	}
}

// ============ 客户 CRUD ============

// ListCustomers 获取客户列表（普通用户按行级隔离过滤）
func ListCustomers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var customers []models.Customer
	var err error
	user := CurrentUser(r)
	if user != nil && !user.IsAdmin() {
		customers, err = models.GetAllCustomersOwnedBy(user.ID)
	} else {
		customers, err = models.GetAllCustomers()
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(customers)
}

// GetCustomer 获取单个客户
func GetCustomer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || id <= 0 {
		http.Error(w, "Invalid id", http.StatusBadRequest)
		return
	}
	customer, err := models.GetCustomerByID(id)
	if err != nil {
		http.Error(w, "Customer not found", http.StatusNotFound)
		return
	}
	if !canViewCustomer(CurrentUser(r), customer) {
		writeJSONError(w, http.StatusForbidden, "无权查看该客户")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(customer)
}

// AddCustomer 添加客户
func AddCustomer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req customerReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	normalizeCustomerReq(&req)
	if err := validateCustomerReq(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	user := CurrentUser(r)
	id, code, err := models.AddCustomer(customerFromReq(&req), user.ID)
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate entry") {
			writeJSONError(w, http.StatusBadRequest, "客户编号已存在，请更换后重试或留空自动生成")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "code": code, "message": "客户添加成功"})
}

// UpdateCustomer 更新客户
func UpdateCustomer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req customerReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	normalizeCustomerReq(&req)
	if req.ID <= 0 {
		writeJSONError(w, http.StatusBadRequest, "Invalid id")
		return
	}
	existing, err := models.GetCustomerByID(req.ID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "客户不存在")
		return
	}
	if !canManageCustomer(CurrentUser(r), existing) {
		writeJSONError(w, http.StatusForbidden, "无权编辑该客户")
		return
	}
	if err := validateCustomerReq(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	user := CurrentUser(r)
	if err := models.UpdateCustomer(customerFromReq(&req), user.ID); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "客户更新成功"})
}

// DeleteCustomer 删除客户
func DeleteCustomer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || id <= 0 {
		http.Error(w, "Invalid id", http.StatusBadRequest)
		return
	}
	existing, err := models.GetCustomerByID(id)
	if err != nil {
		http.Error(w, "Customer not found", http.StatusNotFound)
		return
	}
	if !canManageCustomer(CurrentUser(r), existing) {
		writeJSONError(w, http.StatusForbidden, "无权删除该客户")
		return
	}
	cnt, err := models.CountOrdersByCustomer(id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if cnt > 0 {
		writeJSONError(w, http.StatusBadRequest, "该客户已关联订单，无法删除；可将客户状态改为“终止合作”以保留档案")
		return
	}
	if err := models.DeleteCustomer(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "客户删除成功"})
}

// ============ 客户联系人 ============

type contactReq struct {
	ID            int    `json:"id"`
	CustomerID    int    `json:"customer_id"`
	Name          string `json:"name"`
	Title         string `json:"title"`
	Mobile        string `json:"mobile"`
	Phone         string `json:"phone"`
	Email         string `json:"email"`
	Wechat        string `json:"wechat"`
	Whatsapp      string `json:"whatsapp"`
	SocialAccount string `json:"social_account"`
	IsPrimary     bool   `json:"is_primary"`
	Remark        string `json:"remark"`
}

func normalizeContactReq(req *contactReq) {
	req.Name = strings.TrimSpace(req.Name)
	req.Title = strings.TrimSpace(req.Title)
	req.Mobile = strings.TrimSpace(req.Mobile)
	req.Phone = strings.TrimSpace(req.Phone)
	req.Email = strings.TrimSpace(req.Email)
	req.Wechat = strings.TrimSpace(req.Wechat)
	req.Whatsapp = strings.TrimSpace(req.Whatsapp)
	req.SocialAccount = strings.TrimSpace(req.SocialAccount)
	req.Remark = strings.TrimSpace(req.Remark)
	if req.Title != "" && !containsStr(models.ContactTitles, req.Title) {
		req.Title = ""
	}
}

// ListCustomerContacts 获取客户联系人列表
func ListCustomerContacts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	customerID, err := strconv.Atoi(r.URL.Query().Get("customer_id"))
	if err != nil || customerID <= 0 {
		writeJSONError(w, http.StatusBadRequest, "Invalid customer_id")
		return
	}
	customer, err := models.GetCustomerByID(customerID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "客户不存在")
		return
	}
	if !canViewCustomer(CurrentUser(r), customer) {
		writeJSONError(w, http.StatusForbidden, "无权查看该客户")
		return
	}
	contacts, err := models.ListCustomerContacts(customerID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(contacts)
}

// AddCustomerContact 新增联系人
func AddCustomerContact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req contactReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	normalizeContactReq(&req)
	if req.CustomerID <= 0 {
		writeJSONError(w, http.StatusBadRequest, "Invalid customer_id")
		return
	}
	if req.Name == "" {
		writeJSONError(w, http.StatusBadRequest, "联系人姓名不能为空")
		return
	}
	customer, err := models.GetCustomerByID(req.CustomerID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "客户不存在")
		return
	}
	if !canManageCustomer(CurrentUser(r), customer) {
		writeJSONError(w, http.StatusForbidden, "无权为该客户添加联系人")
		return
	}
	id, err := models.AddCustomerContact(models.CustomerContact{
		CustomerID: req.CustomerID, Name: req.Name, Title: req.Title,
		Mobile: req.Mobile, Phone: req.Phone, Email: req.Email,
		Wechat: req.Wechat, Whatsapp: req.Whatsapp, SocialAccount: req.SocialAccount,
		IsPrimary: req.IsPrimary, Remark: req.Remark,
	})
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "message": "联系人添加成功"})
}

// UpdateCustomerContact 更新联系人
func UpdateCustomerContact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req contactReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	normalizeContactReq(&req)
	if req.ID <= 0 {
		writeJSONError(w, http.StatusBadRequest, "Invalid id")
		return
	}
	if req.Name == "" {
		writeJSONError(w, http.StatusBadRequest, "联系人姓名不能为空")
		return
	}
	existing, err := models.GetCustomerContactByID(req.ID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "联系人不存在")
		return
	}
	customer, err := models.GetCustomerByID(existing.CustomerID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "客户不存在")
		return
	}
	if !canManageCustomer(CurrentUser(r), customer) {
		writeJSONError(w, http.StatusForbidden, "无权修改该客户联系人")
		return
	}
	existing.Name = req.Name
	existing.Title = req.Title
	existing.Mobile = req.Mobile
	existing.Phone = req.Phone
	existing.Email = req.Email
	existing.Wechat = req.Wechat
	existing.Whatsapp = req.Whatsapp
	existing.SocialAccount = req.SocialAccount
	existing.IsPrimary = req.IsPrimary
	existing.Remark = req.Remark
	if err := models.UpdateCustomerContact(*existing); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "联系人更新成功"})
}

// DeleteCustomerContact 删除联系人
func DeleteCustomerContact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || id <= 0 {
		writeJSONError(w, http.StatusBadRequest, "Invalid id")
		return
	}
	contact, err := models.GetCustomerContactByID(id)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "联系人不存在")
		return
	}
	customer, err := models.GetCustomerByID(contact.CustomerID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "客户不存在")
		return
	}
	if !canManageCustomer(CurrentUser(r), customer) {
		writeJSONError(w, http.StatusForbidden, "无权删除该客户联系人")
		return
	}
	if err := models.DeleteCustomerContact(id); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "联系人删除成功"})
}

// ============ 客户附件 ============

// ListCustomerAttachments 获取客户附件列表
func ListCustomerAttachments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	customerID, err := strconv.Atoi(r.URL.Query().Get("customer_id"))
	if err != nil || customerID <= 0 {
		writeJSONError(w, http.StatusBadRequest, "Invalid customer_id")
		return
	}
	customer, err := models.GetCustomerByID(customerID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "客户不存在")
		return
	}
	if !canViewCustomer(CurrentUser(r), customer) {
		writeJSONError(w, http.StatusForbidden, "无权查看该客户")
		return
	}
	attachments, err := models.ListCustomerAttachments(customerID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(attachments)
}

// UploadCustomerAttachment 上传客户附件（分类 + 文件）
func UploadCustomerAttachment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 30<<20)
	if err := r.ParseMultipartForm(30 << 20); err != nil {
		http.Error(w, "读取上传文件失败", http.StatusBadRequest)
		return
	}

	customerID, err := strconv.Atoi(r.FormValue("customer_id"))
	if err != nil || customerID <= 0 {
		writeJSONError(w, http.StatusBadRequest, "Invalid customer_id")
		return
	}
	customer, err := models.GetCustomerByID(customerID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "客户不存在")
		return
	}
	if !canManageCustomer(CurrentUser(r), customer) {
		writeJSONError(w, http.StatusForbidden, "无权为该客户上传附件")
		return
	}

	category := strings.TrimSpace(r.FormValue("category"))
	if category == "" || !containsStr(customerAttachmentCategories, category) {
		category = "其他"
	}

	var fileHeader *multipart.FileHeader
	if r.MultipartForm != nil {
		files := r.MultipartForm.File["file"]
		if len(files) == 0 {
			files = r.MultipartForm.File["files"]
		}
		if len(files) > 0 {
			fileHeader = files[0]
		}
	}
	if fileHeader == nil {
		writeJSONError(w, http.StatusBadRequest, "请选择要上传的文件")
		return
	}

	ext := strings.ToLower(filepath.Ext(fileHeader.Filename))
	if !customerAttachmentExts[ext] {
		writeJSONError(w, http.StatusBadRequest, "不支持的文件类型，仅支持图片、PDF、Office 文档、txt、zip/rar")
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer file.Close()

	dir := filepath.Join("uploads", "customer_files", strconv.Itoa(customerID))
	if err := os.MkdirAll(dir, 0755); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	storedName := fmt.Sprintf("%d_%d%s", time.Now().UnixNano(), 0, ext)
	dst := filepath.Join(dir, storedName)
	out, err := os.Create(dst)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := io.Copy(out, file); err != nil {
		out.Close()
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out.Close()

	user := CurrentUser(r)
	attID, err := models.AddCustomerAttachment(models.CustomerAttachment{
		CustomerID:       customerID,
		Category:         category,
		FileName:         fileHeader.Filename,
		StoredName:       storedName,
		FileSize:         fileHeader.Size,
		Mime:             fileHeader.Header.Get("Content-Type"),
		UploadedByUserID: user.ID,
	})
	if err != nil {
		os.Remove(dst)
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":      attID,
		"message": "附件上传成功",
	})
}

// DeleteCustomerAttachment 删除客户附件
func DeleteCustomerAttachment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || id <= 0 {
		writeJSONError(w, http.StatusBadRequest, "Invalid id")
		return
	}
	att, err := models.GetCustomerAttachmentByID(id)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "附件不存在")
		return
	}
	customer, err := models.GetCustomerByID(att.CustomerID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "客户不存在")
		return
	}
	if !canManageCustomer(CurrentUser(r), customer) {
		writeJSONError(w, http.StatusForbidden, "无权删除该客户附件")
		return
	}
	if err := models.DeleteCustomerAttachment(id); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// 删除磁盘文件（不存在则忽略）
	localPath := filepath.Join("uploads", "customer_files", strconv.Itoa(att.CustomerID), att.StoredName)
	if err := os.Remove(localPath); err != nil && !os.IsNotExist(err) {
		// 仅记录，不阻塞接口返回
		_ = err
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "附件删除成功"})
}
