package models

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Expense 费用记录。
type Expense struct {
	ID                 int       `json:"id"`
	ExpenseNo          string    `json:"expense_no"`
	ExpenseDate        time.Time `json:"expense_date"`
	ExpenseMonth       string    `json:"expense_month"`
	Category           string    `json:"category"`
	Amount             float64   `json:"amount"`
	TaxAmount          *float64  `json:"tax_amount"`
	AmountExcludingTax *float64  `json:"amount_excluding_tax"`
	CounterpartyType   string    `json:"counterparty_type"`
	CounterpartyName   string    `json:"counterparty_name"`
	PaymentStatus      string    `json:"payment_status"`
	PaymentMethod      string    `json:"payment_method"`
	Vouchers           []string  `json:"vouchers"`
	Remark             string    `json:"remark"`
	CreatedByUserID    *int      `json:"created_by_user_id"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// EnsureExpensesTable 创建费用记录表（如果不存在）。
func EnsureExpensesTable() error {
	_, err := DB.Exec(`
        CREATE TABLE IF NOT EXISTS expenses (
            id                       INT AUTO_INCREMENT PRIMARY KEY COMMENT '费用ID',
            expense_no               VARCHAR(32) NOT NULL UNIQUE COMMENT '费用编号',
            expense_date             DATE NOT NULL COMMENT '费用日期',
            expense_month            CHAR(7) NOT NULL COMMENT '所属月份(YYYY-MM)',
            category                 VARCHAR(30) NOT NULL COMMENT '费用类别',
            amount                   DECIMAL(12,2) NOT NULL DEFAULT 0 COMMENT '金额(含税)',
            tax_amount               DECIMAL(12,2) NULL COMMENT '税额',
            amount_excluding_tax     DECIMAL(12,2) NULL COMMENT '不含税金额',
            counterparty_type        VARCHAR(20) NOT NULL DEFAULT '' COMMENT '往来对象类型',
            counterparty_name        VARCHAR(100) NOT NULL DEFAULT '' COMMENT '往来对象名称',
            payment_status           VARCHAR(20) NOT NULL DEFAULT '未付款' COMMENT '付款状态',
            payment_method           VARCHAR(30) NOT NULL DEFAULT '' COMMENT '支付方式',
            vouchers                 TEXT COMMENT '发票或支付凭证路径(JSON数组)',
            remark                   TEXT COMMENT '备注',
            created_by_user_id       INT NULL COMMENT '创建人用户ID',
            created_at               DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
            updated_at               DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
            INDEX idx_expenses_date (expense_date),
            INDEX idx_expenses_month (expense_month),
            INDEX idx_expenses_category (category),
            INDEX idx_expenses_payment_status (payment_status)
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='费用记录表'
    `)
	if err != nil {
		return err
	}
	if err := ensureIndex("expenses", "idx_expenses_date", "expense_date"); err != nil {
		return err
	}
	if err := ensureIndex("expenses", "idx_expenses_month", "expense_month"); err != nil {
		return err
	}
	if err := ensureIndex("expenses", "idx_expenses_category", "category"); err != nil {
		return err
	}
	return ensureIndex("expenses", "idx_expenses_payment_status", "payment_status")
}

func parseExpenseVouchers(raw string) []string {
	if raw == "" {
		return []string{}
	}
	var vouchers []string
	if err := json.Unmarshal([]byte(raw), &vouchers); err != nil || vouchers == nil {
		return []string{}
	}
	return vouchers
}

func marshalExpenseVouchers(vouchers []string) (string, error) {
	if vouchers == nil {
		vouchers = []string{}
	}
	data, err := json.Marshal(vouchers)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// GetAllExpenses 获取全部费用记录，按费用日期倒序排列。
func GetAllExpenses() ([]Expense, error) {
	rows, err := DB.Query(`
        SELECT id, expense_no, expense_date, expense_month, category, amount,
               tax_amount, amount_excluding_tax, counterparty_type, counterparty_name,
               payment_status, payment_method, COALESCE(vouchers, ''), COALESCE(remark, ''),
               created_by_user_id, created_at, updated_at
        FROM expenses
        ORDER BY expense_date DESC, id DESC
    `)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	expenses := []Expense{}
	for rows.Next() {
		expense, err := scanExpense(rows)
		if err != nil {
			return nil, err
		}
		expenses = append(expenses, *expense)
	}
	return expenses, rows.Err()
}

// GetExpenseByID 根据 ID 获取费用记录。
func GetExpenseByID(id int) (*Expense, error) {
	row := DB.QueryRow(`
        SELECT id, expense_no, expense_date, expense_month, category, amount,
               tax_amount, amount_excluding_tax, counterparty_type, counterparty_name,
               payment_status, payment_method, COALESCE(vouchers, ''), COALESCE(remark, ''),
               created_by_user_id, created_at, updated_at
        FROM expenses
        WHERE id = ?
    `, id)
	return scanExpense(row)
}

type expenseScanner interface {
	Scan(dest ...interface{}) error
}

func scanExpense(scanner expenseScanner) (*Expense, error) {
	var expense Expense
	var taxAmount, amountExcludingTax sql.NullFloat64
	var createdByUserID sql.NullInt64
	var vouchersRaw string
	if err := scanner.Scan(
		&expense.ID, &expense.ExpenseNo, &expense.ExpenseDate, &expense.ExpenseMonth,
		&expense.Category, &expense.Amount, &taxAmount, &amountExcludingTax,
		&expense.CounterpartyType, &expense.CounterpartyName, &expense.PaymentStatus,
		&expense.PaymentMethod, &vouchersRaw, &expense.Remark, &createdByUserID,
		&expense.CreatedAt, &expense.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if taxAmount.Valid {
		value := taxAmount.Float64
		expense.TaxAmount = &value
	}
	if amountExcludingTax.Valid {
		value := amountExcludingTax.Float64
		expense.AmountExcludingTax = &value
	}
	if createdByUserID.Valid {
		value := int(createdByUserID.Int64)
		expense.CreatedByUserID = &value
	}
	expense.Vouchers = parseExpenseVouchers(vouchersRaw)
	return &expense, nil
}

// CreateExpense 新增费用记录；费用编号为空时自动生成。
func CreateExpense(expense *Expense) (int64, error) {
	vouchersJSON, err := marshalExpenseVouchers(expense.Vouchers)
	if err != nil {
		return 0, err
	}

	tx, err := DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	placeholderNo := fmt.Sprintf("TMP-%d", time.Now().UnixNano())
	result, err := tx.Exec(`
        INSERT INTO expenses
            (expense_no, expense_date, expense_month, category, amount, tax_amount,
             amount_excluding_tax, counterparty_type, counterparty_name, payment_status,
             payment_method, vouchers, remark, created_by_user_id)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `, placeholderNo, expense.ExpenseDate, expense.ExpenseMonth, expense.Category, expense.Amount,
		expense.TaxAmount, expense.AmountExcludingTax, expense.CounterpartyType, expense.CounterpartyName,
		expense.PaymentStatus, expense.PaymentMethod, vouchersJSON, expense.Remark, expense.CreatedByUserID)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}

	if expense.ExpenseNo == "" {
		expense.ExpenseNo = fmt.Sprintf("FY%s-%05d", expense.ExpenseDate.Format("20060102"), id)
	}
	if _, err := tx.Exec("UPDATE expenses SET expense_no = ? WHERE id = ?", expense.ExpenseNo, id); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	expense.ID = int(id)
	return id, nil
}

// UpdateExpense 更新费用记录。
func UpdateExpense(expense *Expense) error {
	vouchersJSON, err := marshalExpenseVouchers(expense.Vouchers)
	if err != nil {
		return err
	}
	result, err := DB.Exec(`
        UPDATE expenses SET
            expense_no = ?, expense_date = ?, expense_month = ?, category = ?, amount = ?,
            tax_amount = ?, amount_excluding_tax = ?, counterparty_type = ?, counterparty_name = ?,
            payment_status = ?, payment_method = ?, vouchers = ?, remark = ?
        WHERE id = ?
    `, expense.ExpenseNo, expense.ExpenseDate, expense.ExpenseMonth, expense.Category, expense.Amount,
		expense.TaxAmount, expense.AmountExcludingTax, expense.CounterpartyType, expense.CounterpartyName,
		expense.PaymentStatus, expense.PaymentMethod, vouchersJSON, expense.Remark, expense.ID)
	if err != nil {
		return err
	}
	if _, err := result.RowsAffected(); err != nil {
		return err
	}
	return nil
}

// DeleteExpense 删除费用记录。
func DeleteExpense(id int) error {
	result, err := DB.Exec("DELETE FROM expenses WHERE id = ?", id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}
