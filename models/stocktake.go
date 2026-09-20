package models

import (
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	StocktakeStatusDraft     = "draft"
	StocktakeStatusSubmitted = "submitted"
	StocktakeStatusPosted    = "posted"
	StocktakeStatusCancelled = "cancelled"

	StocktakeScopeAll         = "all"
	StocktakeScopeProduct     = "product"
	StocktakeScopeRawMaterial = "raw_material"
)

type StocktakeOrderSummary struct {
	ID              int64      `json:"id"`
	StocktakeNo     string     `json:"stocktake_no"`
	StocktakeDate   string     `json:"stocktake_date"`
	ScopeType       string     `json:"scope_type"`
	Status          string     `json:"status"`
	Remark          string     `json:"remark"`
	CreatedByUserID int        `json:"created_by_user_id"`
	CreatedByName   string     `json:"created_by_name"`
	CountedByName   string     `json:"counted_by_name"`
	PostedByName    string     `json:"posted_by_name"`
	PostedAt        *time.Time `json:"posted_at"`
	CreatedAt       time.Time  `json:"created_at"`
	ItemCount       int        `json:"item_count"`
	PendingCount    int        `json:"pending_count"`
	VarianceCount   int        `json:"variance_count"`
}

type StocktakeItem struct {
	ID               int64    `json:"id"`
	StocktakeOrderID int64    `json:"stocktake_order_id"`
	ItemType         string   `json:"item_type"`
	ItemID           int      `json:"item_id"`
	ItemName         string   `json:"item_name"`
	ItemSpec         string   `json:"item_spec"`
	ItemUnit         string   `json:"item_unit"`
	BookQty          float64  `json:"book_qty"`
	CountedQty       *float64 `json:"counted_qty"`
	VarianceQty      *float64 `json:"variance_qty"`
	UnitCost         float64  `json:"unit_cost"`
	VarianceAmount   float64  `json:"variance_amount"`
	Reason           string   `json:"reason"`
	Remark           string   `json:"remark"`
}

type StocktakeOrder struct {
	StocktakeOrderSummary
	Items []StocktakeItem `json:"items"`
}

type StocktakeLineInput struct {
	ID         int64    `json:"id"`
	CountedQty *float64 `json:"counted_qty"`
	Reason     string   `json:"reason"`
	Remark     string   `json:"remark"`
}

func EnsureStocktakeTables() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS stocktake_order_sequences (
			stocktake_date DATE NOT NULL PRIMARY KEY,
			current_no INT UNSIGNED NOT NULL DEFAULT 0
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='盘点单按日序号表'`,
		`CREATE TABLE IF NOT EXISTS stocktake_orders (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			stocktake_no VARCHAR(32) NOT NULL,
			stocktake_date DATE NOT NULL,
			scope_type VARCHAR(20) NOT NULL DEFAULT 'all',
			status VARCHAR(20) NOT NULL DEFAULT 'draft',
			counted_by INT NOT NULL DEFAULT 0,
			reviewed_by INT NOT NULL DEFAULT 0,
			posted_by INT NOT NULL DEFAULT 0,
			posted_at DATETIME NULL,
			remark VARCHAR(500) NOT NULL DEFAULT '',
			created_by_user_id INT NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE KEY uniq_stocktake_no (stocktake_no),
			KEY idx_stocktake_date_status (stocktake_date, status)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='库存盘点单'`,
		`CREATE TABLE IF NOT EXISTS stocktake_items (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			stocktake_order_id BIGINT NOT NULL,
			item_type VARCHAR(20) NOT NULL,
			item_id INT NOT NULL,
			item_name VARCHAR(200) NOT NULL DEFAULT '',
			item_spec VARCHAR(100) NOT NULL DEFAULT '',
			item_unit VARCHAR(20) NOT NULL DEFAULT '',
			book_qty DECIMAL(14,3) NOT NULL DEFAULT 0,
			counted_qty DECIMAL(14,3) NULL,
			variance_qty DECIMAL(14,3) NULL,
			unit_cost DECIMAL(14,4) NOT NULL DEFAULT 0,
			variance_amount DECIMAL(14,2) NOT NULL DEFAULT 0,
			reason VARCHAR(300) NOT NULL DEFAULT '',
			remark VARCHAR(500) NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE KEY uniq_stocktake_item (stocktake_order_id, item_type, item_id),
			KEY idx_stocktake_item_order (stocktake_order_id, item_type, item_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='库存盘点明细'`,
	}
	for _, statement := range statements {
		if _, err := DB.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func validStocktakeScope(scope string) bool {
	return scope == StocktakeScopeAll || scope == StocktakeScopeProduct || scope == StocktakeScopeRawMaterial
}

func validateStocktakeRemark(remark string) error {
	if utf8.RuneCountInString(strings.TrimSpace(remark)) > 500 {
		return fmt.Errorf("盘点说明不能超过500个字符")
	}
	return nil
}

func validateStocktakeLine(line *StocktakeLineInput, itemType string) error {
	if line.ID <= 0 {
		return fmt.Errorf("盘点明细ID无效")
	}
	if itemType != InventoryItemProduct && itemType != InventoryItemRawMaterial {
		return fmt.Errorf("盘点物料类型无效")
	}
	if line.CountedQty != nil {
		value := *line.CountedQty
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("实盘数量无效")
		}
		if value < 0 {
			return fmt.Errorf("实盘数量不能小于0")
		}
		if itemType == InventoryItemProduct && math.Abs(value-math.Round(value)) > 0.0005 {
			return fmt.Errorf("成品实盘数量必须为整数")
		}
		if value > 99999999999.999 {
			return fmt.Errorf("实盘数量超出允许范围")
		}
	}
	line.Reason = strings.TrimSpace(line.Reason)
	line.Remark = strings.TrimSpace(line.Remark)
	if utf8.RuneCountInString(line.Reason) > 300 {
		return fmt.Errorf("差异原因不能超过300个字符")
	}
	if utf8.RuneCountInString(line.Remark) > 500 {
		return fmt.Errorf("明细备注不能超过500个字符")
	}
	return nil
}

func dateOnly(value time.Time) time.Time {
	year, month, day := value.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.Local)
}

// stocktakeDateString preserves the business date when MySQL writes a DATE value.
func stocktakeDateString(value time.Time) string {
	return dateOnly(value).Format("2006-01-02")
}

func NextStocktakeNoTx(tx *sql.Tx, stocktakeDate time.Time) (string, error) {
	stocktakeDate = dateOnly(stocktakeDate)
	stocktakeDateValue := stocktakeDateString(stocktakeDate)
	if _, err := tx.Exec(`
		INSERT INTO stocktake_order_sequences (stocktake_date, current_no)
		VALUES (?, 0)
		ON DUPLICATE KEY UPDATE current_no = current_no
	`, stocktakeDateValue); err != nil {
		return "", err
	}
	var current int
	if err := tx.QueryRow(`
		SELECT current_no FROM stocktake_order_sequences WHERE stocktake_date = ? FOR UPDATE
	`, stocktakeDateValue).Scan(&current); err != nil {
		return "", err
	}
	if current >= 9999 {
		return "", fmt.Errorf("当天盘点单号已用完")
	}
	current++
	if _, err := tx.Exec(`
		UPDATE stocktake_order_sequences SET current_no = ? WHERE stocktake_date = ?
	`, current, stocktakeDateValue); err != nil {
		return "", err
	}
	return fmt.Sprintf("PD%s%04d", stocktakeDate.Format("20060102"), current), nil
}

func CreateStocktake(stocktakeDate time.Time, scopeType, remark string, createdBy int) (*StocktakeOrder, error) {
	stocktakeDate = dateOnly(stocktakeDate)
	stocktakeDateValue := stocktakeDateString(stocktakeDate)
	if !validStocktakeScope(scopeType) {
		return nil, fmt.Errorf("盘点范围无效")
	}
	if err := validateStocktakeRemark(remark); err != nil {
		return nil, err
	}
	tx, err := DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	no, err := NextStocktakeNoTx(tx, stocktakeDate)
	if err != nil {
		return nil, err
	}
	res, err := tx.Exec(`
		INSERT INTO stocktake_orders
			(stocktake_no, stocktake_date, scope_type, status, remark, created_by_user_id)
		VALUES (?, ?, ?, 'draft', ?, ?)
	`, no, stocktakeDateValue, scopeType, remark, createdBy)
	if err != nil {
		return nil, err
	}
	orderID, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	if scopeType == StocktakeScopeAll || scopeType == StocktakeScopeProduct {
		if _, err := tx.Exec(`
			INSERT INTO stocktake_items
				(stocktake_order_id, item_type, item_id, item_name, item_spec, item_unit, book_qty, unit_cost)
			SELECT ?, 'product', p.id, p.name, p.spec, p.unit, p.stock, p.avg_cost
			FROM products p ORDER BY p.id
		`, orderID); err != nil {
			return nil, err
		}
	}
	if scopeType == StocktakeScopeAll || scopeType == StocktakeScopeRawMaterial {
		if _, err := tx.Exec(`
			INSERT INTO stocktake_items
				(stocktake_order_id, item_type, item_id, item_name, item_spec, item_unit, book_qty, unit_cost)
			SELECT ?, 'raw_material', rm.id, rm.name, rm.spec, rm.unit, rm.stock, rm.avg_cost
			FROM raw_materials rm ORDER BY rm.id
		`, orderID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return GetStocktake(orderID)
}

func ListStocktakes(includeProduct, includeMaterial bool) ([]StocktakeOrderSummary, error) {
	scopeCondition := "1 = 0"
	switch {
	case includeProduct && includeMaterial:
		scopeCondition = "o.scope_type IN ('all', 'product', 'raw_material')"
	case includeProduct:
		scopeCondition = "o.scope_type = 'product'"
	case includeMaterial:
		scopeCondition = "o.scope_type = 'raw_material'"
	default:
		return []StocktakeOrderSummary{}, nil
	}
	query := `
		SELECT o.id, o.stocktake_no,
		       DATE_FORMAT(o.stocktake_date, '%Y-%m-%d') AS stocktake_date,
		       o.scope_type, o.status, o.remark,
		       o.created_by_user_id,
		       COALESCE(NULLIF(cu.display_name, ''), cu.username, '') AS created_by_name,
		       COALESCE(NULLIF(cou.display_name, ''), cou.username, '') AS counted_by_name,
		       COALESCE(NULLIF(pu.display_name, ''), pu.username, '') AS posted_by_name,
		       o.posted_at, o.created_at,
		       COUNT(i.id) AS item_count,
		       COALESCE(SUM(i.counted_qty IS NULL), 0) AS pending_count,
		       COALESCE(SUM(ABS(COALESCE(i.variance_qty, 0)) > 0.0005), 0) AS variance_count
		FROM stocktake_orders o
		LEFT JOIN stocktake_items i ON i.stocktake_order_id = o.id
		LEFT JOIN users cu ON cu.id = o.created_by_user_id
		LEFT JOIN users cou ON cou.id = o.counted_by
		LEFT JOIN users pu ON pu.id = o.posted_by
		WHERE ` + scopeCondition + `
		GROUP BY o.id, o.stocktake_no, o.stocktake_date, o.scope_type, o.status, o.remark,
		         o.created_by_user_id, cu.display_name, cu.username, cou.display_name, cou.username,
		         pu.display_name, pu.username, o.posted_at, o.created_at
		ORDER BY o.id DESC LIMIT 300
	`
	rows, err := DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []StocktakeOrderSummary{}
	for rows.Next() {
		var item StocktakeOrderSummary
		if err := rows.Scan(&item.ID, &item.StocktakeNo, &item.StocktakeDate, &item.ScopeType,
			&item.Status, &item.Remark, &item.CreatedByUserID, &item.CreatedByName,
			&item.CountedByName, &item.PostedByName, &item.PostedAt, &item.CreatedAt,
			&item.ItemCount, &item.PendingCount, &item.VarianceCount); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	return list, rows.Err()
}

func getStocktakeSummary(scanner interface {
	Scan(dest ...interface{}) error
}) (*StocktakeOrder, error) {
	var order StocktakeOrder
	err := scanner.Scan(&order.ID, &order.StocktakeNo, &order.StocktakeDate, &order.ScopeType,
		&order.Status, &order.Remark, &order.CreatedByUserID, &order.CreatedByName,
		&order.CountedByName, &order.PostedByName, &order.PostedAt, &order.CreatedAt)
	if err != nil {
		return nil, err
	}
	order.Items = []StocktakeItem{}
	return &order, nil
}

func GetStocktake(id int64) (*StocktakeOrder, error) {
	order, err := getStocktakeSummary(DB.QueryRow(`
		SELECT o.id, o.stocktake_no,
		       DATE_FORMAT(o.stocktake_date, '%Y-%m-%d') AS stocktake_date,
		       o.scope_type, o.status, o.remark, o.created_by_user_id,
		       COALESCE(NULLIF(cu.display_name, ''), cu.username, '') AS created_by_name,
		       COALESCE(NULLIF(cou.display_name, ''), cou.username, '') AS counted_by_name,
		       COALESCE(NULLIF(pu.display_name, ''), pu.username, '') AS posted_by_name,
		       o.posted_at, o.created_at
		FROM stocktake_orders o
		LEFT JOIN users cu ON cu.id = o.created_by_user_id
		LEFT JOIN users cou ON cou.id = o.counted_by
		LEFT JOIN users pu ON pu.id = o.posted_by
		WHERE o.id = ?
	`, id))
	if err != nil {
		return nil, err
	}
	rows, err := DB.Query(`
		SELECT id, stocktake_order_id, item_type, item_id, item_name, item_spec, item_unit,
		       book_qty, counted_qty, variance_qty, unit_cost, variance_amount, reason, remark
		FROM stocktake_items
		WHERE stocktake_order_id = ?
		ORDER BY item_type, item_id
	`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item StocktakeItem
		var counted, variance sql.NullFloat64
		if err := rows.Scan(&item.ID, &item.StocktakeOrderID, &item.ItemType, &item.ItemID,
			&item.ItemName, &item.ItemSpec, &item.ItemUnit, &item.BookQty, &counted,
			&variance, &item.UnitCost, &item.VarianceAmount, &item.Reason, &item.Remark); err != nil {
			return nil, err
		}
		if counted.Valid {
			value := counted.Float64
			item.CountedQty = &value
		}
		if variance.Valid {
			value := variance.Float64
			item.VarianceQty = &value
		}
		order.Items = append(order.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	order.ItemCount = len(order.Items)
	for _, item := range order.Items {
		if item.CountedQty == nil {
			order.PendingCount++
		}
		if item.VarianceQty != nil && (*item.VarianceQty > 0.0005 || *item.VarianceQty < -0.0005) {
			order.VarianceCount++
		}
	}
	return order, nil
}

func UpdateStocktakeDraft(id int64, stocktakeDate time.Time, remark string, lines []StocktakeLineInput, userID int) (*StocktakeOrder, error) {
	if err := validateStocktakeRemark(remark); err != nil {
		return nil, err
	}
	stocktakeDate = dateOnly(stocktakeDate)
	stocktakeDateValue := stocktakeDateString(stocktakeDate)
	tx, err := DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var status string
	if err := tx.QueryRow("SELECT status FROM stocktake_orders WHERE id = ? FOR UPDATE", id).Scan(&status); err != nil {
		return nil, err
	}
	if status != StocktakeStatusDraft {
		return nil, fmt.Errorf("只有草稿盘点单可以修改")
	}
	if _, err := tx.Exec(`
		UPDATE stocktake_orders
		SET stocktake_date = ?, remark = ?, counted_by = ?
		WHERE id = ?
	`, stocktakeDateValue, remark, userID, id); err != nil {
		return nil, err
	}
	for i := range lines {
		line := &lines[i]
		var ownerID int64
		var itemType string
		if err := tx.QueryRow(`
			SELECT stocktake_order_id, item_type FROM stocktake_items WHERE id = ? FOR UPDATE
		`, line.ID).Scan(&ownerID, &itemType); err != nil {
			if err == sql.ErrNoRows {
				return nil, fmt.Errorf("盘点明细不存在")
			}
			return nil, err
		}
		if ownerID != id {
			return nil, fmt.Errorf("盘点明细不属于当前盘点单")
		}
		if err := validateStocktakeLine(line, itemType); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(`
			UPDATE stocktake_items
			SET counted_qty = ?,
			    variance_qty = CASE WHEN ? IS NULL THEN NULL ELSE ? - book_qty END,
			    variance_amount = CASE WHEN ? IS NULL THEN 0 ELSE (? - book_qty) * unit_cost END,
			    reason = ?, remark = ?
			WHERE id = ? AND stocktake_order_id = ?
		`, line.CountedQty, line.CountedQty, line.CountedQty, line.CountedQty, line.CountedQty,
			line.Reason, line.Remark, line.ID, id); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return GetStocktake(id)
}

func SubmitStocktake(id int64, userID int) (*StocktakeOrder, error) {
	tx, err := DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var status string
	if err := tx.QueryRow(`
		SELECT status FROM stocktake_orders WHERE id = ? FOR UPDATE
	`, id).Scan(&status); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("盘点单不存在")
		}
		return nil, err
	}
	if status != StocktakeStatusDraft {
		return nil, fmt.Errorf("只有草稿盘点单可以提交")
	}
	var itemCount, pendingCount int
	if err := tx.QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(counted_qty IS NULL), 0)
		FROM stocktake_items WHERE stocktake_order_id = ?
	`, id).Scan(&itemCount, &pendingCount); err != nil {
		return nil, err
	}
	if itemCount == 0 {
		return nil, fmt.Errorf("盘点范围内没有可提交的物料")
	}
	if pendingCount > 0 {
		return nil, fmt.Errorf("还有 %d 项未录入实盘数量", pendingCount)
	}
	if _, err := tx.Exec(`
		UPDATE stocktake_orders
		SET status = 'submitted', counted_by = ?
		WHERE id = ? AND status = 'draft'
	`, userID, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return GetStocktake(id)
}

func CancelStocktake(id int64, userID int) (*StocktakeOrder, error) {
	res, err := DB.Exec(`
		UPDATE stocktake_orders
		SET reviewed_by = IF(status = 'submitted', ?, reviewed_by),
		    status = 'cancelled'
		WHERE id = ? AND status IN ('draft', 'submitted')
	`, userID, id)
	if err != nil {
		return nil, err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return nil, fmt.Errorf("已过账或已取消的盘点单不能取消")
	}
	return GetStocktake(id)
}

func PostStocktake(id int64, userID int) (*StocktakeOrder, error) {
	tx, err := DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var status string
	var stocktakeDate time.Time
	var stocktakeNo string
	if err := tx.QueryRow(`
		SELECT status, stocktake_date, stocktake_no
		FROM stocktake_orders WHERE id = ? FOR UPDATE
	`, id).Scan(&status, &stocktakeDate, &stocktakeNo); err != nil {
		return nil, err
	}
	if status != StocktakeStatusSubmitted {
		return nil, fmt.Errorf("只有已提交盘点单可以审核过账")
	}
	type line struct {
		ID         int64
		ItemType   string
		ItemID     int
		BookQty    float64
		CountedQty sql.NullFloat64
		Reason     string
		Remark     string
	}
	rows, err := tx.Query(`
		SELECT id, item_type, item_id, book_qty, counted_qty, reason, remark
		FROM stocktake_items WHERE stocktake_order_id = ?
		ORDER BY item_type, item_id FOR UPDATE
	`, id)
	if err != nil {
		return nil, err
	}
	var lines []line
	for rows.Next() {
		var item line
		if err := rows.Scan(&item.ID, &item.ItemType, &item.ItemID, &item.BookQty,
			&item.CountedQty, &item.Reason, &item.Remark); err != nil {
			rows.Close()
			return nil, err
		}
		lines = append(lines, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("盘点单没有可过账的明细")
	}
	now := time.Now()
	occurredAt := time.Date(stocktakeDate.Year(), stocktakeDate.Month(), stocktakeDate.Day(),
		now.Hour(), now.Minute(), now.Second(), 0, time.Local)
	for _, item := range lines {
		if !item.CountedQty.Valid {
			return nil, fmt.Errorf("盘点明细尚未录入实盘数量")
		}
		if item.ItemType == InventoryItemProduct && math.Abs(item.CountedQty.Float64-math.Round(item.CountedQty.Float64)) > 0.0005 {
			return nil, fmt.Errorf("成品实盘数量必须为整数")
		}
		table, err := inventoryTable(item.ItemType)
		if err != nil {
			return nil, err
		}
		var currentStock, unitCost float64
		if err := tx.QueryRow(fmt.Sprintf("SELECT stock, avg_cost FROM `%s` WHERE id = ? FOR UPDATE", table), item.ItemID).Scan(&currentStock, &unitCost); err != nil {
			return nil, err
		}
		delta := canonicalQty(item.CountedQty.Float64 - currentStock)
		varianceAmount := canonicalMoney(delta * unitCost)
		if _, err := tx.Exec(`
			UPDATE stocktake_items
			SET book_qty = ?, counted_qty = ?, variance_qty = ?, unit_cost = ?, variance_amount = ?
			WHERE id = ?
		`, canonicalQty(currentStock), canonicalQty(item.CountedQty.Float64), delta, unitCost, varianceAmount, item.ID); err != nil {
			return nil, err
		}
		if delta > 0.0005 || delta < -0.0005 {
			movementType := MovementStocktakeIn
			if delta < 0 {
				movementType = MovementStocktakeOut
			}
			remark := item.Reason
			if item.Remark != "" {
				if remark != "" {
					remark += "；"
				}
				remark += item.Remark
			}
			if remark == "" {
				remark = "库存盘点差异调整"
			}
			if _, err := ApplyStockDeltaTx(tx, StockMovementInput{
				ItemType: item.ItemType, ItemID: item.ItemID, Quantity: delta,
				UnitCost: unitCost, MovementType: movementType,
				ReferenceType: "stocktake", ReferenceID: id, ReferenceNo: stocktakeNo,
				OccurredAt: occurredAt, BatchNo: stocktakeNo, Remark: remark,
				CreatedByUserID: userID, OverrideUnitCost: true,
			}); err != nil {
				return nil, err
			}
		}
	}
	if _, err := tx.Exec(`
		UPDATE stocktake_orders
		SET status = 'posted', reviewed_by = ?, posted_by = ?, posted_at = ?
		WHERE id = ?
		`, userID, userID, now, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return GetStocktake(id)
}
