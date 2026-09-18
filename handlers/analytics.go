package handlers

import (
	"encoding/json"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"order-system/models"
)

type analyticsSummary struct {
	Revenue      float64 `json:"revenue"`
	Cost         float64 `json:"cost"`
	Profit       float64 `json:"profit"`
	CostRatio    float64 `json:"cost_ratio"`
	ProfitMargin float64 `json:"profit_margin"`
	OrderCount   int     `json:"order_count"`
	Quantity     float64 `json:"quantity"`
}

type analyticsMonthlyRow struct {
	Period       string  `json:"period"`
	Revenue      float64 `json:"revenue"`
	Cost         float64 `json:"cost"`
	Profit       float64 `json:"profit"`
	CostRatio    float64 `json:"cost_ratio"`
	ProfitMargin float64 `json:"profit_margin"`
	OrderCount   int     `json:"order_count"`
	Quantity     float64 `json:"quantity"`
}

type analyticsProductRow struct {
	ProductID    int     `json:"product_id"`
	ProductName  string  `json:"product_name"`
	Spec         string  `json:"spec"`
	Unit         string  `json:"unit"`
	Quantity     float64 `json:"quantity"`
	Revenue      float64 `json:"revenue"`
	Cost         float64 `json:"cost"`
	Profit       float64 `json:"profit"`
	ProfitMargin float64 `json:"profit_margin"`
	RevenueShare float64 `json:"revenue_share"`
	ProfitShare  float64 `json:"profit_share"`
	OrderCount   int     `json:"order_count"`
}

type analyticsAccumulator struct {
	Revenue  float64
	Cost     float64
	Quantity float64
	OrderIDs map[int]struct{}
}

func newAnalyticsAccumulator() *analyticsAccumulator {
	return &analyticsAccumulator{OrderIDs: make(map[int]struct{})}
}

func (a *analyticsAccumulator) add(orderID int, revenue, cost, quantity float64) {
	a.Revenue += revenue
	a.Cost += cost
	a.Quantity += quantity
	a.OrderIDs[orderID] = struct{}{}
}

type analyticsProductAccumulator struct {
	ProductID   int
	ProductName string
	Spec        string
	Unit        string
	Revenue     float64
	Cost        float64
	Quantity    float64
	OrderIDs    map[int]struct{}
}

func newAnalyticsProductAccumulator(productID int, name, spec, unit string) *analyticsProductAccumulator {
	return &analyticsProductAccumulator{
		ProductID:   productID,
		ProductName: name,
		Spec:        spec,
		Unit:        unit,
		OrderIDs:    make(map[int]struct{}),
	}
}

func (a *analyticsProductAccumulator) add(orderID int, revenue, cost, quantity float64) {
	a.Revenue += revenue
	a.Cost += cost
	a.Quantity += quantity
	a.OrderIDs[orderID] = struct{}{}
}

func analyticsRatio(numerator, denominator float64) float64 {
	if denominator == 0 {
		return 0
	}
	return numerator / denominator * 100
}

func roundAnalytics(value float64, digits int) float64 {
	scale := math.Pow10(digits)
	return math.Round(value*scale) / scale
}

// GetAnalyticsOverview 返回已发货订单的收入、材料成本、毛利及产品盈利排行。
// 范围参数 range=month/year/all；未传时默认按月。
func GetAnalyticsOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rangeType := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("range")))
	if rangeType == "" {
		rangeType = "month"
	}
	if rangeType != "month" && rangeType != "year" && rangeType != "all" {
		writeJSONError(w, http.StatusBadRequest, "统计范围不正确")
		return
	}

	month := strings.TrimSpace(r.URL.Query().Get("month"))
	if month == "" {
		month = time.Now().Format("2006-01")
	}
	if _, err := time.Parse("2006-01", month); err != nil {
		writeJSONError(w, http.StatusBadRequest, "月份格式不正确")
		return
	}
	year := month[:4]

	scopeLabel := month
	matchesScope := func(period string) bool {
		switch rangeType {
		case "month":
			return period == month
		case "year":
			return strings.HasPrefix(period, year+"-")
		default:
			return true
		}
	}
	if rangeType == "month" {
		scopeLabel = year + "年" + month[5:] + "月"
	} else if rangeType == "year" {
		scopeLabel = year + "年"
	} else {
		scopeLabel = "全部已发货订单"
	}

	user := CurrentUser(r)
	perms := PermissionsFor(user)
	viewAll := perms[PermOrderViewAll] || perms[PermOrderEditAll]

	query := `
		SELECT DATE_FORMAT(o.shipped_date, '%Y-%m') AS period,
		       o.id AS order_id,
		       oi.product_id,
		       COALESCE(p.name, CONCAT('产品#', oi.product_id)) AS product_name,
		       COALESCE(p.spec, '') AS spec,
		       COALESCE(p.unit, '') AS unit,
		       oi.quantity,
		       oi.quantity * oi.price AS revenue,
		       CASE WHEN cost.movement_count > 0 THEN cost.actual_cost ELSE COALESCE(fallback.unit_cost * oi.quantity, 0) END AS cost
		FROM orders o
		JOIN order_items oi ON oi.order_id = o.id
		LEFT JOIN products p ON p.id = oi.product_id
		LEFT JOIN (
			SELECT order_item_id, -SUM(total_cost) AS actual_cost, COUNT(*) AS movement_count
			FROM stock_movements
			WHERE item_type = 'product'
			  AND order_item_id > 0
			  AND movement_type IN ('sales_out', 'sales_out_reversal', 'legacy_sales_cost')
			GROUP BY order_item_id
		) cost ON cost.order_item_id = oi.id
		LEFT JOIN (
			SELECT pb.product_id,
			       SUM(pb.quantity * COALESCE(NULLIF(rm.avg_cost, 0), rm.price, 0)) AS unit_cost
			FROM product_bom pb
			LEFT JOIN raw_materials rm ON rm.id = pb.raw_material_id
			GROUP BY pb.product_id
		) fallback ON fallback.product_id = oi.product_id
		WHERE o.status = 3 AND o.shipped_date IS NOT NULL`
	args := []interface{}{}
	if !viewAll {
		if user == nil {
			writeJSONError(w, http.StatusForbidden, "没有权限查看数据分析")
			return
		}
		query += " AND (o.created_by_user_id = ? OR o.owner_user_id = ?)"
		args = append(args, user.ID, user.ID)
	}

	rows, err := models.DB.Query(query, args...)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	summary := newAnalyticsAccumulator()
	monthly := make(map[string]*analyticsAccumulator)
	products := make(map[int]*analyticsProductAccumulator)

	for rows.Next() {
		var period string
		var orderID, productID int
		var productName, spec, unit string
		var quantity, revenue, cost float64
		if err := rows.Scan(&period, &orderID, &productID, &productName, &spec, &unit, &quantity, &revenue, &cost); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}

		monthlyAcc := monthly[period]
		if monthlyAcc == nil {
			monthlyAcc = newAnalyticsAccumulator()
			monthly[period] = monthlyAcc
		}
		monthlyAcc.add(orderID, revenue, cost, quantity)

		if !matchesScope(period) {
			continue
		}

		summary.add(orderID, revenue, cost, quantity)
		productAcc := products[productID]
		if productAcc == nil {
			productAcc = newAnalyticsProductAccumulator(productID, productName, spec, unit)
			products[productID] = productAcc
		}
		productAcc.add(orderID, revenue, cost, quantity)
	}
	if err := rows.Err(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	summaryResult := analyticsSummary{
		Revenue:      roundAnalytics(summary.Revenue, 2),
		Cost:         roundAnalytics(summary.Cost, 2),
		Profit:       roundAnalytics(summary.Revenue-summary.Cost, 2),
		CostRatio:    roundAnalytics(analyticsRatio(summary.Cost, summary.Revenue), 2),
		ProfitMargin: roundAnalytics(analyticsRatio(summary.Revenue-summary.Cost, summary.Revenue), 2),
		OrderCount:   len(summary.OrderIDs),
		Quantity:     roundAnalytics(summary.Quantity, 3),
	}

	monthlyRows := make([]analyticsMonthlyRow, 0, len(monthly))
	for period, acc := range monthly {
		monthlyRows = append(monthlyRows, analyticsMonthlyRow{
			Period:       period,
			Revenue:      roundAnalytics(acc.Revenue, 2),
			Cost:         roundAnalytics(acc.Cost, 2),
			Profit:       roundAnalytics(acc.Revenue-acc.Cost, 2),
			CostRatio:    roundAnalytics(analyticsRatio(acc.Cost, acc.Revenue), 2),
			ProfitMargin: roundAnalytics(analyticsRatio(acc.Revenue-acc.Cost, acc.Revenue), 2),
			OrderCount:   len(acc.OrderIDs),
			Quantity:     roundAnalytics(acc.Quantity, 3),
		})
	}
	sort.Slice(monthlyRows, func(i, j int) bool { return monthlyRows[i].Period < monthlyRows[j].Period })

	totalRevenue := summary.Revenue
	totalProfit := summary.Revenue - summary.Cost
	productRows := make([]analyticsProductRow, 0, len(products))
	for _, acc := range products {
		profit := acc.Revenue - acc.Cost
		profitShare := 0.0
		if totalProfit > 0 {
			profitShare = analyticsRatio(profit, totalProfit)
		}
		productRows = append(productRows, analyticsProductRow{
			ProductID:    acc.ProductID,
			ProductName:  acc.ProductName,
			Spec:         acc.Spec,
			Unit:         acc.Unit,
			Quantity:     roundAnalytics(acc.Quantity, 3),
			Revenue:      roundAnalytics(acc.Revenue, 2),
			Cost:         roundAnalytics(acc.Cost, 2),
			Profit:       roundAnalytics(profit, 2),
			ProfitMargin: roundAnalytics(analyticsRatio(profit, acc.Revenue), 2),
			RevenueShare: roundAnalytics(analyticsRatio(acc.Revenue, totalRevenue), 2),
			ProfitShare:  roundAnalytics(profitShare, 2),
			OrderCount:   len(acc.OrderIDs),
		})
	}
	sort.Slice(productRows, func(i, j int) bool {
		if productRows[i].Profit == productRows[j].Profit {
			return productRows[i].Revenue > productRows[j].Revenue
		}
		return productRows[i].Profit > productRows[j].Profit
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"scope": map[string]string{
			"range": rangeType,
			"month": month,
			"year":  year,
			"label": scopeLabel,
		},
		"summary":  summaryResult,
		"monthly":  monthlyRows,
		"products": productRows,
	})
}
