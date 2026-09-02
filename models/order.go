package models

import "time"

type Order struct {
	ID                   int        `json:"id"`
	OrderNo              string     `json:"order_no"`
	CustomerName         string     `json:"customer_name"`
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
	PaymentSettlement    string     `json:"payment_settlement"`
	FreightPayment       string     `json:"freight_payment"`
	FreightRecovery      string     `json:"freight_recovery"`
	TransportMethod      string     `json:"transport_method"`
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
