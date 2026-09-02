package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"order-system/models"
)

// GetOrderSummary 返回订单月度/年度金额汇总（仅统计已发货订单，按实际发货日期归属），
// 与采购物料管理页面的汇总风格一致。
// month 参数格式 YYYY-MM，缺省为当月；年度统计取该月所在年份。
// 仅本人范围的用户只统计自己创建的订单。
func GetOrderSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	month := strings.TrimSpace(r.URL.Query().Get("month"))
	if month == "" {
		month = time.Now().Format("2006-01")
	}
	if _, err := time.Parse("2006-01", month); err != nil {
		http.Error(w, "月份格式不正确", http.StatusBadRequest)
		return
	}
	year := month[:4]

	user := CurrentUser(r)
	perms := PermissionsFor(user)
	viewAll := perms[PermOrderViewAll] || perms[PermOrderEditAll]

	base := " FROM orders WHERE status = 3 AND shipped_date IS NOT NULL"
	args := []interface{}{}
	if !viewAll {
		if user == nil {
			writeJSONError(w, http.StatusForbidden, "没有权限查看订单汇总")
			return
		}
		base += " AND created_by_user_id = ?"
		args = append(args, user.ID)
	}

	query := func(periodSQL string, arg string) (int, float64, error) {
		var count int
		var amount float64
		q := "SELECT COUNT(*), COALESCE(SUM(total_amount), 0)" + base + " AND " + periodSQL
		err := models.DB.QueryRow(q, append(append([]interface{}{}, args...), arg)...).Scan(&count, &amount)
		return count, amount, err
	}

	monthCount, monthAmount, err := query("DATE_FORMAT(shipped_date, '%Y-%m') = ?", month)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	yearCount, yearAmount, err := query("DATE_FORMAT(shipped_date, '%Y') = ?", year)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"month":         month,
		"year":          year,
		"monthly_total": monthAmount,
		"yearly_total":  yearAmount,
		"monthly_count": monthCount,
		"yearly_count":  yearCount,
	})
}
