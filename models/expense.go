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
	if _, err := DB.Exec(`
        CREATE TABLE IF NOT EXISTS expense_sequences (
            expense_date  DATE NOT NULL PRIMARY KEY COMMENT '费用日期',
            current_no    INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '当天已使用的最大序号'
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='费用编号按日序号表'
    `); err != nil {
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

	expenseNo := expense.ExpenseNo
	if expenseNo == "" {
		expenseNo, err = NextExpenseNoTx(tx, expense.ExpenseDate)
		if err != nil {
			return 0, err
		}
	}
	result, err := tx.Exec(`
        INSERT INTO expenses
            (expense_no, expense_date, expense_month, category, amount, tax_amount,
             amount_excluding_tax, counterparty_type, counterparty_name, payment_status,
             payment_method, vouchers, remark, created_by_user_id)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `, expenseNo, expense.ExpenseDate, expense.ExpenseMonth, expense.Category, expense.Amount,
		expense.TaxAmount, expense.AmountExcludingTax, expense.CounterpartyType, expense.CounterpartyName,
		expense.PaymentStatus, expense.PaymentMethod, vouchersJSON, expense.Remark, expense.CreatedByUserID)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	expense.ID = int(id)
	expense.ExpenseNo = expenseNo
	return id, nil
}

const (
	expenseNoPrefix           = "FY"
	expenseNoMaxDailySequence = 9999
)

// NextExpenseNoTx 按费用日期生成“FY+YYYYMMDD+4位当天序号”，例如 FY202609190001。
// 序号表行锁保证同一日期并发新增时不会生成重复编号，并会兼容已有无前缀编号。
func NextExpenseNoTx(tx *sql.Tx, expenseDate time.Time) (string, error) {
	datePart := expenseDate.Format("20060102")
	if _, err := tx.Exec(`
        INSERT INTO expense_sequences (expense_date, current_no)
        VALUES (?, 0)
        ON DUPLICATE KEY UPDATE current_no = current_no
    `, expenseDate); err != nil {
		return "", err
	}

	var currentNo int
	if err := tx.QueryRow(`
        SELECT current_no
        FROM expense_sequences
        WHERE expense_date = ?
        FOR UPDATE
    `, expenseDate).Scan(&currentNo); err != nil {
		return "", err
	}

	var existingMax int
	if err := tx.QueryRow(`
        SELECT COALESCE(MAX(CAST(RIGHT(expense_no, 4) AS UNSIGNED)), 0)
        FROM expenses
        WHERE (
                (CHAR_LENGTH(expense_no) = 14 AND expense_no REGEXP '^FY[0-9]{12}$')
             OR (CHAR_LENGTH(expense_no) = 12 AND expense_no REGEXP '^[0-9]{12}$')
            )
          AND SUBSTRING(expense_no, IF(LEFT(expense_no, 2) = 'FY', 3, 1), 8) = ?
    `, datePart).Scan(&existingMax); err != nil {
		return "", err
	}
	if existingMax > currentNo {
		currentNo = existingMax
	}
	if currentNo >= expenseNoMaxDailySequence {
		return "", fmt.Errorf("当天费用编号已用完（最多%d条）", expenseNoMaxDailySequence)
	}

	nextNo := currentNo + 1
	if _, err := tx.Exec(`
        UPDATE expense_sequences
        SET current_no = ?
        WHERE expense_date = ?
    `, nextNo, expenseDate); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%s%04d", expenseNoPrefix, datePart, nextNo), nil
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
