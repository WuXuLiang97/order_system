package models

import (
	"database/sql"
	"fmt"
	"time"
)

type Order struct {
	ID                   int        `json:"id"`
	OrderNo              string     `json:"order_no"`
	CustomerID           int        `json:"customer_id"`
	CustomerName         string     `json:"customer_name"`
	CustomerCode         string     `json:"customer_code"`
	CustomerFullName     string     `json:"customer_full_name"`
	Region               string     `json:"region"`
	CustomerAddress      string     `json:"customer_address"`
	CustomerPhone        string     `json:"customer_phone"`
	OrderDate            *time.Time `json:"order_date"`
	ExpectedShippingDate *time.Time `json:"expected_shipping_date"`
	CustomerRequiredDate *time.Time `json:"customer_required_date"`
	LogisticsDays        int        `json:"logistics_days"`
	DeliveryDate         *time.Time `json:"delivery_date"`
	TotalAmount          float64    `json:"total_amount"`
	Status               int        `json:"status"`
	PaymentStatus        int        `json:"payment_status"`
	PreparedBy           string     `json:"prepared_by"`
	CreatedByUserID      *int       `json:"created_by_user_id"`
	OwnerUserID          int        `json:"owner_user_id"`
	OwnerName            string     `json:"owner_name"`
	PaymentSettlement    string     `json:"payment_settlement"`
	FreightPayment       string     `json:"freight_payment"`
	FreightRecovery      string     `json:"freight_recovery"`
	TransportMethod      string     `json:"transport_method"`
	Currency             string     `json:"currency"`
	TradeTerms           string     `json:"trade_terms"`
	ShippingMark         string     `json:"shipping_mark"`
	Remark               string     `json:"remark"`
	WarningLevel         string     `json:"warning_level"`
	WarningLabel         string     `json:"warning_label"`
	CreatedAt            time.Time  `json:"created_at"`
}

type OrderItem struct {
	ID        int     `json:"id"`
	OrderID   int     `json:"order_id"`
	ProductID int     `json:"product_id"`
	Quantity  int     `json:"quantity"`
	Price     float64 `json:"price"`
}

// EnsureOrderSequencesTable 创建订单号按日序号表（如果不存在）。
func EnsureOrderSequencesTable() error {
	_, err := DB.Exec(`
        CREATE TABLE IF NOT EXISTS order_sequences (
            order_date  DATE NOT NULL PRIMARY KEY COMMENT '下单日期',
            current_no  INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '当天已使用的最大序号'
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='订单号按日序号表'
    `)
	return err
}

const (
	orderNoPrefix           = "YKL"
	orderNoMaxDailySequence = 9999
)

// NextOrderNoTx 按下单日期生成“YKL+YYYYMMDD+4位当天序号”，例如 YKL202609190001。
// 序号表行锁保证同一日期并发新增时不会生成重复单号，并会兼容已有无前缀单号。
func NextOrderNoTx(tx *sql.Tx, orderDate time.Time) (string, error) {
	datePart := orderDate.Format("20060102")
	if _, err := tx.Exec(`
        INSERT INTO order_sequences (order_date, current_no)
        VALUES (?, 0)
        ON DUPLICATE KEY UPDATE current_no = current_no
    `, orderDate); err != nil {
		return "", err
	}

	var currentNo int
	if err := tx.QueryRow(`
        SELECT current_no
        FROM order_sequences
        WHERE order_date = ?
        FOR UPDATE
    `, orderDate).Scan(&currentNo); err != nil {
		return "", err
	}

	var existingMax int
	if err := tx.QueryRow(`
        SELECT COALESCE(MAX(CAST(RIGHT(order_no, 4) AS UNSIGNED)), 0)
        FROM orders
        WHERE (
                (CHAR_LENGTH(order_no) = 15 AND order_no REGEXP '^YKL[0-9]{12}$')
             OR (CHAR_LENGTH(order_no) = 12 AND order_no REGEXP '^[0-9]{12}$')
            )
          AND SUBSTRING(order_no, IF(LEFT(order_no, 3) = 'YKL', 4, 1), 8) = ?
    `, datePart).Scan(&existingMax); err != nil {
		return "", err
	}
	if existingMax > currentNo {
		currentNo = existingMax
	}
	if currentNo >= orderNoMaxDailySequence {
		return "", fmt.Errorf("当天订单号已用完（最多%d单）", orderNoMaxDailySequence)
	}

	nextNo := currentNo + 1
	if _, err := tx.Exec(`
        UPDATE order_sequences
        SET current_no = ?
        WHERE order_date = ?
    `, nextNo, orderDate); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%s%04d", orderNoPrefix, datePart, nextNo), nil
}
