package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"order-system/models"
)

var expenseCategories = map[string]bool{
	"厂房租金":  true,
	"员工工资":  true,
	"社保公积金": true,
	"水电费":   true,
	"物流运费":  true,
	"办公费":   true,
	"差旅费":   true,
	"加工费":   true,
	"服务费":   true,
	"其他支出":  true,
}

var expenseCounterpartyTypes = map[string]bool{
	"房东":  true,
	"员工":  true,
	"服务商": true,
}

var expensePaymentStatuses = map[string]bool{
	"未付款":  true,
	"部分付款": true,
	"已付款":  true,
}

var expensePaymentMethods = map[string]bool{
	"银行转账": true,
	"微信":   true,
	"支付宝":  true,
	"现金":   true,
	"支票":   true,
	"其他":   true,
}

var expenseVoucherExtensions = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".pdf": true,
}

type expenseRequest struct {
	ID                 int      `json:"id"`
	ExpenseNo          string   `json:"expense_no"`
	ExpenseDate        string   `json:"expense_date"`
	ExpenseMonth       string   `json:"expense_month"`
	Category           string   `json:"category"`
	Amount             float64  `json:"amount"`
	TaxAmount          *float64 `json:"tax_amount"`
	AmountExcludingTax *float64 `json:"amount_excluding_tax"`
	CounterpartyType   string   `json:"counterparty_type"`
	CounterpartyName   string   `json:"counterparty_name"`
	PaymentStatus      string   `json:"payment_status"`
	PaymentMethod      string   `json:"payment_method"`
	Vouchers           []string `json:"vouchers"`
	Remark             string   `json:"remark"`
}

func normalizeExpenseVoucherURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.Contains(raw, "\\") {
		return "", false
	}
	clean := path.Clean("/" + strings.TrimPrefix(raw, "/"))
	const prefix = "/uploads/expense_vouchers/"
	if !strings.HasPrefix(clean, prefix) {
		return "", false
	}
	if !expenseVoucherExtensions[strings.ToLower(path.Ext(clean))] {
		return "", false
	}
	return clean, true
}

func normalizeExpenseVouchers(vouchers []string) ([]string, bool) {
	normalized := make([]string, 0, len(vouchers))
	seen := map[string]bool{}
	for _, voucher := range vouchers {
		clean, ok := normalizeExpenseVoucherURL(voucher)
		if !ok {
			return nil, false
		}
		if seen[clean] {
			continue
		}
		seen[clean] = true
		normalized = append(normalized, clean)
		if len(normalized) > 20 {
			return nil, false
		}
	}
	return normalized, true
}

func buildExpense(req expenseRequest, existing *models.Expense, user *models.User) (*models.Expense, string) {
	expenseDate, err := time.Parse("2006-01-02", strings.TrimSpace(req.ExpenseDate))
	if err != nil {
		return nil, "费用日期格式不正确"
	}

	expenseNo := strings.TrimSpace(req.ExpenseNo)
	if existing != nil && expenseNo == "" {
		expenseNo = existing.ExpenseNo
	}
	if len([]rune(expenseNo)) > 32 {
		return nil, "费用编号不能超过32个字符"
	}

	expenseMonth := strings.TrimSpace(req.ExpenseMonth)
	if expenseMonth == "" {
		expenseMonth = expenseDate.Format("2006-01")
	}
	if _, err := time.Parse("2006-01", expenseMonth); err != nil {
		return nil, "所属月份格式不正确"
	}

	if req.Amount <= 0 {
		return nil, "金额必须大于0"
	}
	if req.TaxAmount != nil && (*req.TaxAmount < 0 || *req.TaxAmount > req.Amount) {
		return nil, "税额必须在0和金额之间"
	}
	if req.AmountExcludingTax != nil && (*req.AmountExcludingTax < 0 || *req.AmountExcludingTax > req.Amount) {
		return nil, "不含税金额必须在0和金额之间"
	}

	category := strings.TrimSpace(req.Category)
	if !expenseCategories[category] {
		return nil, "费用类别不正确"
	}
	counterpartyType := strings.TrimSpace(req.CounterpartyType)
	if !expenseCounterpartyTypes[counterpartyType] {
		return nil, "请选择正确的往来对象类型"
	}
	counterpartyName := strings.TrimSpace(req.CounterpartyName)
	if len([]rune(counterpartyName)) > 100 {
		return nil, "往来对象名称不能超过100个字符"
	}

	paymentStatus := strings.TrimSpace(req.PaymentStatus)
	if paymentStatus == "" {
		paymentStatus = "未付款"
	}
	if !expensePaymentStatuses[paymentStatus] {
		return nil, "付款状态不正确"
	}
	paymentMethod := strings.TrimSpace(req.PaymentMethod)
	if paymentMethod == "" {
		paymentMethod = "银行转账"
	}
	if !expensePaymentMethods[paymentMethod] {
		return nil, "支付方式不正确"
	}

	vouchers, ok := normalizeExpenseVouchers(req.Vouchers)
	if !ok {
		return nil, "发票或支付凭证地址不正确"
	}

	expense := &models.Expense{
		ExpenseNo:          expenseNo,
		ExpenseDate:        expenseDate,
		ExpenseMonth:       expenseMonth,
		Category:           category,
		Amount:             req.Amount,
		TaxAmount:          req.TaxAmount,
		AmountExcludingTax: req.AmountExcludingTax,
		CounterpartyType:   counterpartyType,
		CounterpartyName:   counterpartyName,
		PaymentStatus:      paymentStatus,
		PaymentMethod:      paymentMethod,
		Vouchers:           vouchers,
		Remark:             strings.TrimSpace(req.Remark),
	}
	if existing != nil {
		expense.ID = existing.ID
		expense.CreatedByUserID = existing.CreatedByUserID
	} else if user != nil {
		userID := user.ID
		expense.CreatedByUserID = &userID
	}
	return expense, ""
}

// ListExpenses 返回全部费用记录。
func ListExpenses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	expenses, err := models.GetAllExpenses()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(expenses)
}

// GetExpense 返回单条费用记录。
func GetExpense(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || id <= 0 {
		http.Error(w, "Invalid id", http.StatusBadRequest)
		return
	}
	expense, err := models.GetExpenseByID(id)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "费用记录不存在", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(expense)
}

// AddExpense 新增费用记录。
func AddExpense(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req expenseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求格式错误", http.StatusBadRequest)
		return
	}
	expense, message := buildExpense(req, nil, CurrentUser(r))
	if message != "" {
		http.Error(w, message, http.StatusBadRequest)
		return
	}
	id, err := models.CreateExpense(expense)
	if err != nil {
		writeExpenseSaveError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":         id,
		"expense_no": expense.ExpenseNo,
		"message":    "费用记录添加成功",
	})
}

// UpdateExpense 更新费用记录。
func UpdateExpense(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req expenseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求格式错误", http.StatusBadRequest)
		return
	}
	if req.ID <= 0 {
		http.Error(w, "Invalid id", http.StatusBadRequest)
		return
	}
	existing, err := models.GetExpenseByID(req.ID)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "费用记录不存在", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	expense, message := buildExpense(req, existing, CurrentUser(r))
	if message != "" {
		http.Error(w, message, http.StatusBadRequest)
		return
	}
	if err := models.UpdateExpense(expense); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "费用记录不存在", http.StatusNotFound)
			return
		}
		writeExpenseSaveError(w, err)
		return
	}

	if stale := staleExpenseVouchers(existing.Vouchers, expense.Vouchers); len(stale) > 0 {
		deleteExpenseVoucherFiles(stale)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "费用记录更新成功"})
}

// DeleteExpense 删除费用记录及其凭证文件。
func DeleteExpense(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || id <= 0 {
		http.Error(w, "Invalid id", http.StatusBadRequest)
		return
	}
	expense, err := models.GetExpenseByID(id)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "费用记录不存在", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := models.DeleteExpense(id); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "费用记录不存在", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	deleteExpenseVoucherFiles(expense.Vouchers)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "费用记录删除成功"})
}

func writeExpenseSaveError(w http.ResponseWriter, err error) {
	if strings.Contains(err.Error(), "Duplicate entry") {
		http.Error(w, "费用编号已存在", http.StatusConflict)
		return
	}
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

func staleExpenseVouchers(oldVouchers, newVouchers []string) []string {
	current := map[string]bool{}
	for _, voucher := range newVouchers {
		current[voucher] = true
	}
	var stale []string
	for _, voucher := range oldVouchers {
		if !current[voucher] {
			stale = append(stale, voucher)
		}
	}
	return stale
}

func deleteExpenseVoucherFiles(vouchers []string) {
	for _, voucher := range vouchers {
		clean, ok := normalizeExpenseVoucherURL(voucher)
		if !ok {
			continue
		}
		localPath := filepath.FromSlash(strings.TrimPrefix(clean, "/"))
		if err := os.Remove(localPath); err != nil && !os.IsNotExist(err) {
			log.Printf("删除费用凭证文件失败 %s: %v", localPath, err)
		}
	}
}

// UploadExpenseVoucher 上传发票或支付凭证（图片/PDF）。
func UploadExpenseVoucher(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 30<<20)
	if err := r.ParseMultipartForm(30 << 20); err != nil {
		http.Error(w, "读取上传文件失败", http.StatusBadRequest)
		return
	}

	var fileHeaders []*multipart.FileHeader
	if r.MultipartForm != nil {
		fileHeaders = r.MultipartForm.File["files"]
		if len(fileHeaders) == 0 {
			fileHeaders = r.MultipartForm.File["file"]
		}
	}
	if len(fileHeaders) == 0 {
		http.Error(w, "请选择要上传的发票或支付凭证", http.StatusBadRequest)
		return
	}
	if len(fileHeaders) > 20 {
		http.Error(w, "单次最多上传20个文件", http.StatusBadRequest)
		return
	}

	dir := filepath.Join("uploads", "expense_vouchers")
	if err := os.MkdirAll(dir, 0755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	urls := make([]string, 0, len(fileHeaders))
	for _, header := range fileHeaders {
		ext := strings.ToLower(filepath.Ext(header.Filename))
		if !expenseVoucherExtensions[ext] {
			http.Error(w, "仅支持 jpg、jpeg、png、gif、webp 图片或 pdf 文件", http.StatusBadRequest)
			return
		}
		file, err := header.Open()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		filename := fmt.Sprintf("%d_%d%s", time.Now().UnixNano(), len(urls), ext)
		dst := filepath.Join(dir, filename)
		out, err := os.Create(dst)
		if err != nil {
			file.Close()
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if _, err := io.Copy(out, file); err != nil {
			out.Close()
			file.Close()
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out.Close()
		file.Close()
		urls = append(urls, "/"+filepath.ToSlash(dst))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"urls":    urls,
		"message": "上传成功",
	})
}
