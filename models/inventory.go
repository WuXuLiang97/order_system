package models

import (
	"database/sql"
	"fmt"
	"math"
	"time"
)

const (
	InventoryItemProduct     = "product"
	InventoryItemRawMaterial = "raw_material"

	MovementOpening           = "opening"
	MovementPurchaseIn        = "purchase_in"
	MovementManualIn          = "manual_in"
	MovementManualOut         = "manual_out"
	MovementProductionIn      = "production_in"
	MovementProductionConsume = "production_consume"
	MovementStocktakeIn       = "stocktake_in"
	MovementStocktakeOut      = "stocktake_out"
	MovementSalesOut          = "sales_out"
	MovementSalesOutReversal  = "sales_out_reversal"
	MovementAdjustment        = "adjustment"
	MovementLegacyShortage    = "legacy_shortage"
)

// StockMovementInput 描述一次库存变动。Quantity 带方向：入库为正、出库为负。
type StockMovementInput struct {
	ItemType         string
	ItemID           int
	Quantity         float64
	UnitCost         float64
	MovementType     string
	ReferenceType    string
	ReferenceID      int64
	ReferenceNo      string
	OrderItemID      int
	ReversalOf       int64
	OverrideUnitCost bool
	CreatedByUserID  int
	OccurredAt       time.Time
	BatchNo          string
	Remark           string
}

type StockMovementResult struct {
	MovementID  int64
	StockBefore float64
	NewStock    float64
	UnitCost    float64
	TotalCost   float64
	AvgCost     float64
}

type StockMovement struct {
	ID              int64     `json:"id"`
	ItemType        string    `json:"item_type"`
	ItemID          int       `json:"item_id"`
	MovementType    string    `json:"movement_type"`
	Quantity        float64   `json:"quantity"`
	UnitCost        float64   `json:"unit_cost"`
	TotalCost       float64   `json:"total_cost"`
	StockBefore     *float64  `json:"stock_before"`
	StockAfter      *float64  `json:"stock_after"`
	ReferenceType   string    `json:"reference_type"`
	ReferenceID     int64     `json:"reference_id"`
	ReferenceNo     string    `json:"reference_no"`
	OrderItemID     int       `json:"order_item_id"`
	ReversalOf      int64     `json:"reversal_of"`
	OccurredAt      time.Time `json:"occurred_at"`
	BusinessDate    string    `json:"business_date"`
	BatchNo         string    `json:"batch_no"`
	CreatedByUserID int       `json:"created_by_user_id"`
	Remark          string    `json:"remark"`
	CreatedAt       time.Time `json:"created_at"`
	ItemName        string    `json:"item_name"`
	ItemSpec        string    `json:"item_spec"`
	ItemUnit        string    `json:"item_unit"`
	OperatorName    string    `json:"operator_name"`
}

type StockMovementFilter struct {
	StartDate    string
	EndDate      string
	ItemType     string
	ItemID       int
	Direction    string
	MovementType string
	ReferenceNo  string
	Operator     string
	Limit        int
	Offset       int
}

type ProductAvailability struct {
	Stock      float64
	Reserved   float64
	Available  float64
	OrderOwned float64
}

type PurchaseDemandItem struct {
	RawMaterialID     int     `json:"raw_material_id"`
	Name              string  `json:"name"`
	Spec              string  `json:"spec"`
	Unit              string  `json:"unit"`
	ActualStock       float64 `json:"actual_stock"`
	ReservedStock     float64 `json:"reserved_stock"`
	AvailableStock    float64 `json:"available_stock"`
	ProductionDemand  float64 `json:"production_demand"`
	InTransitPurchase float64 `json:"in_transit_purchase"`
	SafetyStock       float64 `json:"safety_stock"`
	NetDemand         float64 `json:"net_demand"`
	SuggestedPurchase float64 `json:"suggested_purchase"`
	AvgCost           float64 `json:"avg_cost"`
}

// EnsureInventoryAccounting 创建库存流水、订单占用账，并迁移历史负库存。
func EnsureInventoryAccounting() error {
	if err := ensureColumn("products", "avg_cost", "DECIMAL(14,4) NOT NULL DEFAULT 0 COMMENT '移动加权平均成本'"); err != nil {
		return err
	}
	if err := ensureColumn("raw_materials", "avg_cost", "DECIMAL(14,4) NOT NULL DEFAULT 0 COMMENT '移动加权平均成本'"); err != nil {
		return err
	}

	if _, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS stock_movements (
			id                  BIGINT AUTO_INCREMENT PRIMARY KEY,
			item_type           VARCHAR(20) NOT NULL COMMENT 'product/raw_material',
			item_id             INT NOT NULL,
			movement_type       VARCHAR(40) NOT NULL,
			quantity            DECIMAL(14,3) NOT NULL COMMENT '带方向，入库为正出库为负',
			stock_before        DECIMAL(14,3) NULL COMMENT '变动前库存',
			stock_after         DECIMAL(14,3) NULL COMMENT '变动后库存',
			batch_no            VARCHAR(64) NOT NULL DEFAULT '' COMMENT '生产批次或入库批次',
			business_date       DATE NULL COMMENT '业务日期',
			unit_cost           DECIMAL(14,4) NOT NULL DEFAULT 0,
			total_cost          DECIMAL(14,2) NOT NULL DEFAULT 0 COMMENT '带方向',
			reference_type      VARCHAR(40) NOT NULL DEFAULT '',
			reference_id        BIGINT NOT NULL DEFAULT 0,
			reference_no        VARCHAR(64) NOT NULL DEFAULT '',
			order_item_id       INT NOT NULL DEFAULT 0,
			reversal_of         BIGINT NOT NULL DEFAULT 0,
			occurred_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			created_by_user_id  INT NOT NULL DEFAULT 0,
			remark              VARCHAR(500) NOT NULL DEFAULT '',
			created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			KEY idx_stock_movement_item (item_type, item_id, occurred_at),
			KEY idx_stock_movement_business_date (business_date),
			KEY idx_stock_movement_reference (reference_type, reference_id),
			KEY idx_stock_movement_order_item (order_item_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='不可变库存与成本流水'
	`); err != nil {
		return err
	}

	if err := ensureColumn("stock_movements", "stock_before", "DECIMAL(14,3) NULL COMMENT '变动前库存'"); err != nil {
		return err
	}
	if err := ensureColumn("stock_movements", "stock_after", "DECIMAL(14,3) NULL COMMENT '变动后库存'"); err != nil {
		return err
	}
	if err := ensureColumn("stock_movements", "batch_no", "VARCHAR(64) NOT NULL DEFAULT '' COMMENT '生产批次或入库批次'"); err != nil {
		return err
	}
	if err := ensureColumn("stock_movements", "business_date", "DATE NULL COMMENT '业务日期'"); err != nil {
		return err
	}
	if err := ensureIndex("stock_movements", "idx_stock_movement_business_date", "`business_date`"); err != nil {
		return err
	}

	if _, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS inventory_reservations (
			id                 BIGINT AUTO_INCREMENT PRIMARY KEY,
			order_id           INT NOT NULL,
			order_item_id      INT NOT NULL,
			item_type          VARCHAR(20) NOT NULL,
			item_id            INT NOT NULL,
			quantity           DECIMAL(14,3) NOT NULL DEFAULT 0 COMMENT '原占用数量',
			consumed_quantity  DECIMAL(14,3) NOT NULL DEFAULT 0,
			released_quantity  DECIMAL(14,3) NOT NULL DEFAULT 0,
			status             VARCHAR(20) NOT NULL DEFAULT 'active',
			created_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE KEY uniq_inventory_reservation_item (order_item_id, item_type, item_id),
			KEY idx_inventory_reservation_order (order_id),
			KEY idx_inventory_reservation_item_lookup (item_type, item_id, status)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='订单占用账'
	`); err != nil {
		return err
	}

	return migrateInventoryFoundation()
}

func inventoryTable(itemType string) (string, error) {
	switch itemType {
	case InventoryItemProduct:
		return "products", nil
	case InventoryItemRawMaterial:
		return "raw_materials", nil
	default:
		return "", fmt.Errorf("unknown inventory item type %q", itemType)
	}
}

func canonicalQty(value float64) float64 {
	return math.Round(value*1000) / 1000
}

func canonicalMoney(value float64) float64 {
	return math.Round(value*100) / 100
}

// ApplyStockDeltaTx 以事务方式更新实际库存、移动平均成本并记录不可变流水。
func ApplyStockDeltaTx(tx *sql.Tx, input StockMovementInput) (StockMovementResult, error) {
	var result StockMovementResult
	table, err := inventoryTable(input.ItemType)
	if err != nil {
		return result, err
	}
	quantity := canonicalQty(input.Quantity)
	if math.Abs(quantity) < 0.0005 {
		return result, fmt.Errorf("库存变动数量不能为0")
	}
	if input.MovementType == "" {
		return result, fmt.Errorf("库存流水类型不能为空")
	}

	var stock, avgCost float64
	if err := tx.QueryRow(fmt.Sprintf("SELECT stock, avg_cost FROM `%s` WHERE id = ? FOR UPDATE", table), input.ItemID).Scan(&stock, &avgCost); err != nil {
		return result, err
	}
	stockBefore := canonicalQty(stock)

	unitCost := input.UnitCost
	if quantity < 0 {
		if stock+quantity < -0.0005 {
			return result, fmt.Errorf("实际库存不足：当前 %.3f，需要 %.3f", stock, -quantity)
		}
		if stock+quantity < 0 {
			quantity = -stock
		}
		if !input.OverrideUnitCost {
			unitCost = avgCost
		}
	} else if unitCost < 0 {
		return result, fmt.Errorf("单位成本不能小于0")
	}

	newStock := canonicalQty(stock + quantity)
	newAvgCost := avgCost
	if quantity > 0 {
		if newStock > 0.0005 {
			newAvgCost = (stock*avgCost + quantity*unitCost) / newStock
		} else {
			newAvgCost = unitCost
		}
	}
	if newStock < 0 && newStock > -0.0005 {
		newStock = 0
	}

	totalCost := canonicalMoney(quantity * unitCost)
	if _, err := tx.Exec(fmt.Sprintf("UPDATE `%s` SET stock = ?, avg_cost = ? WHERE id = ?", table), newStock, newAvgCost, input.ItemID); err != nil {
		return result, err
	}

	occurredAt := input.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now()
	}
	businessDate := occurredAt.Format("2006-01-02")
	res, err := tx.Exec(`
		INSERT INTO stock_movements
			(item_type, item_id, movement_type, quantity, stock_before, stock_after, batch_no, business_date,
			 unit_cost, total_cost, reference_type, reference_id, reference_no, order_item_id, reversal_of,
			 occurred_at, created_by_user_id, remark)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, input.ItemType, input.ItemID, input.MovementType, quantity, stockBefore, newStock, input.BatchNo, businessDate,
		unitCost, totalCost, input.ReferenceType, input.ReferenceID, input.ReferenceNo, input.OrderItemID, input.ReversalOf,
		occurredAt, input.CreatedByUserID, input.Remark)
	if err != nil {
		return result, err
	}
	movementID, _ := res.LastInsertId()
	result.MovementID = movementID
	result.StockBefore = stockBefore
	result.NewStock = newStock
	result.UnitCost = unitCost
	result.TotalCost = totalCost
	result.AvgCost = newAvgCost
	return result, nil
}

// AddRawMaterialByKeyTx 按名称+规格查找或创建原材料，库存初始为0。
func AddRawMaterialByKeyTx(tx *sql.Tx, name, spec, unit string, price float64) (int, error) {
	var id int
	err := tx.QueryRow("SELECT id FROM raw_materials WHERE name = ? AND COALESCE(spec, '') = ? ORDER BY id ASC LIMIT 1 FOR UPDATE", name, spec).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	res, err := tx.Exec("INSERT INTO raw_materials (name, spec, stock, unit, min_stock, price, avg_cost) VALUES (?, ?, 0, ?, 0, ?, 0)", name, spec, unit, price)
	if err != nil {
		return 0, err
	}
	inserted, err := res.LastInsertId()
	return int(inserted), err
}

func productReservedTx(tx *sql.Tx, productID int, excludeOrderID int) (float64, error) {
	var reserved float64
	query := `
		SELECT COALESCE(SUM(r.quantity - r.consumed_quantity - r.released_quantity), 0)
		FROM inventory_reservations r
		LEFT JOIN order_items oi ON oi.id = r.order_item_id
		WHERE r.item_type = ? AND r.item_id = ? AND r.status = 'active'`
	args := []interface{}{InventoryItemProduct, productID}
	if excludeOrderID > 0 {
		query += " AND (oi.order_id IS NULL OR oi.order_id <> ?)"
		args = append(args, excludeOrderID)
	}
	if err := tx.QueryRow(query, args...).Scan(&reserved); err != nil {
		return 0, err
	}
	return canonicalQty(reserved), nil
}

func orderProductReservedTx(tx *sql.Tx, orderID, productID int) (float64, error) {
	if orderID <= 0 {
		return 0, nil
	}
	var reserved float64
	err := tx.QueryRow(`
		SELECT COALESCE(SUM(r.quantity - r.consumed_quantity - r.released_quantity), 0)
		FROM inventory_reservations r
		JOIN order_items oi ON oi.id = r.order_item_id
		WHERE r.item_type = ? AND r.item_id = ? AND r.status = 'active' AND oi.order_id = ?
	`, InventoryItemProduct, productID, orderID).Scan(&reserved)
	return canonicalQty(reserved), err
}

// ReserveProductForOrderTx 只占用当前可用成品库存，不扣减实际库存。
func ReserveProductForOrderTx(tx *sql.Tx, orderID, orderItemID, productID int, requested float64) (float64, float64, error) {
	if requested <= 0 {
		return 0, 0, nil
	}
	var stock float64
	if err := tx.QueryRow("SELECT stock FROM products WHERE id = ? FOR UPDATE", productID).Scan(&stock); err != nil {
		return 0, 0, err
	}
	reserved, err := productReservedTx(tx, productID, 0)
	if err != nil {
		return 0, 0, err
	}
	available := canonicalQty(stock - reserved)
	reserveQty := canonicalQty(math.Min(requested, math.Max(available, 0)))
	if reserveQty > 0 {
		if _, err := tx.Exec(`
			INSERT INTO inventory_reservations
				(order_id, order_item_id, item_type, item_id, quantity, status)
			VALUES (?, ?, ?, ?, ?, 'active')
			ON DUPLICATE KEY UPDATE
				quantity = quantity + VALUES(quantity),
				released_quantity = GREATEST(0, released_quantity - VALUES(quantity)),
				status = 'active'
		`, orderID, orderItemID, InventoryItemProduct, productID, reserveQty); err != nil {
			return 0, 0, err
		}
	}
	return reserveQty, available, nil
}

// GetProductOutboundAllowanceTx 返回某订单当前可出库量：自有占用 + 未占用可用库存。
func GetProductOutboundAllowanceTx(tx *sql.Tx, productID, orderID int) (ProductAvailability, error) {
	var info ProductAvailability
	if err := tx.QueryRow("SELECT stock FROM products WHERE id = ? FOR UPDATE", productID).Scan(&info.Stock); err != nil {
		return info, err
	}
	reserved, err := productReservedTx(tx, productID, orderID)
	if err != nil {
		return info, err
	}
	owned, err := orderProductReservedTx(tx, orderID, productID)
	if err != nil {
		return info, err
	}
	info.Reserved = reserved
	info.OrderOwned = owned
	info.Available = canonicalQty(info.Stock - reserved)
	return info, nil
}

func ConsumeProductReservationTx(tx *sql.Tx, orderItemID, productID int, quantity float64) error {
	if orderItemID <= 0 || quantity <= 0 {
		return nil
	}
	if _, err := tx.Exec(`
		UPDATE inventory_reservations
		SET consumed_quantity = LEAST(quantity - released_quantity, consumed_quantity + ?)
		WHERE order_item_id = ? AND item_type = ? AND item_id = ? AND status = 'active'
	`, canonicalQty(quantity), orderItemID, InventoryItemProduct, productID); err != nil {
		return err
	}
	_, err := tx.Exec(`
		UPDATE inventory_reservations
		SET status = IF(quantity - consumed_quantity - released_quantity <= 0.0005, 'consumed', 'active')
		WHERE order_item_id = ? AND item_type = ? AND item_id = ? AND status = 'active'
	`, orderItemID, InventoryItemProduct, productID)
	return err
}

func RestoreProductReservationTx(tx *sql.Tx, orderItemID, productID int, quantity float64) error {
	if orderItemID <= 0 || quantity <= 0 {
		return nil
	}
	if _, err := tx.Exec(`
		UPDATE inventory_reservations
		SET consumed_quantity = GREATEST(0, consumed_quantity - ?)
		WHERE order_item_id = ? AND item_type = ? AND item_id = ?
	`, canonicalQty(quantity), orderItemID, InventoryItemProduct, productID); err != nil {
		return err
	}
	_, err := tx.Exec(`
		UPDATE inventory_reservations
		SET status = IF(quantity - consumed_quantity - released_quantity <= 0.0005,
		               IF(released_quantity > 0, 'released', 'consumed'),
		               'active')
		WHERE order_item_id = ? AND item_type = ? AND item_id = ?
	`, orderItemID, InventoryItemProduct, productID)
	return err
}

// ReleaseOrderReservationsTx 取消订单：释放尚未实际出库的占用。
func ReleaseOrderReservationsTx(tx *sql.Tx, orderID int) error {
	_, err := tx.Exec(`
		UPDATE inventory_reservations
		SET released_quantity = quantity - consumed_quantity,
		    status = IF(consumed_quantity > 0, 'consumed', 'released')
		WHERE order_id = ? AND status = 'active'
	`, orderID)
	return err
}

// ReactivateOrderReservationsTx 恢复已取消订单时重新按当前可用库存占用。
func ReactivateOrderReservationsTx(tx *sql.Tx, orderID int) error {
	rows, err := tx.Query(`
		SELECT id, order_item_id, item_id, quantity, consumed_quantity
		FROM inventory_reservations
		WHERE order_id = ? AND item_type = ? AND status IN ('released', 'consumed')
		ORDER BY id ASC
		FOR UPDATE
	`, orderID, InventoryItemProduct)
	if err != nil {
		return err
	}
	type rowData struct {
		id, orderItemID, productID int
		quantity, consumed         float64
	}
	var items []rowData
	for rows.Next() {
		var item rowData
		if err := rows.Scan(&item.id, &item.orderItemID, &item.productID, &item.quantity, &item.consumed); err != nil {
			rows.Close()
			return err
		}
		items = append(items, item)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range items {
		var stock float64
		if err := tx.QueryRow("SELECT stock FROM products WHERE id = ? FOR UPDATE", item.productID).Scan(&stock); err != nil {
			return err
		}
		reserved, err := productReservedTx(tx, item.productID, 0)
		if err != nil {
			return err
		}
		desired := canonicalQty(item.quantity - item.consumed)
		available := canonicalQty(math.Max(stock-reserved, 0))
		reactivated := canonicalQty(math.Min(desired, available))
		newQuantity := canonicalQty(item.consumed + desired)
		released := canonicalQty(desired - reactivated)
		status := "released"
		if reactivated > 0 {
			status = "active"
		} else if item.consumed > 0 {
			status = "consumed"
		}
		if _, err := tx.Exec(`
			UPDATE inventory_reservations
			SET quantity = ?, released_quantity = ?, status = ?
			WHERE id = ?
		`, newQuantity, released, status, item.id); err != nil {
			return err
		}
	}

	// 历史订单或下单时完全没有可用库存的明细，可能尚无占用记录；恢复订单时补建占用账。
	missingRows, err := tx.Query(`
		SELECT oi.id, oi.product_id, oi.quantity,
		       COALESCE((
		           SELECT SUM(poi.quantity)
		           FROM product_outbound_items poi
		           WHERE poi.order_item_id = oi.id
		       ), 0) AS shipped_quantity
		FROM order_items oi
		LEFT JOIN inventory_reservations r
		       ON r.order_item_id = oi.id
		      AND r.item_type = ?
		      AND r.item_id = oi.product_id
		WHERE oi.order_id = ? AND r.id IS NULL
		FOR UPDATE
	`, InventoryItemProduct, orderID)
	if err != nil {
		return err
	}
	type missingRow struct {
		orderItemID, productID int
		ordered, shipped       float64
	}
	var missing []missingRow
	for missingRows.Next() {
		var item missingRow
		if err := missingRows.Scan(&item.orderItemID, &item.productID, &item.ordered, &item.shipped); err != nil {
			missingRows.Close()
			return err
		}
		missing = append(missing, item)
	}
	if err := missingRows.Close(); err != nil {
		return err
	}
	for _, item := range missing {
		remaining := canonicalQty(math.Max(item.ordered-item.shipped, 0))
		if remaining <= 0 {
			continue
		}
		var stock float64
		if err := tx.QueryRow("SELECT stock FROM products WHERE id = ? FOR UPDATE", item.productID).Scan(&stock); err != nil {
			return err
		}
		reserved, err := productReservedTx(tx, item.productID, 0)
		if err != nil {
			return err
		}
		available := canonicalQty(math.Max(stock-reserved, 0))
		reactivated := canonicalQty(math.Min(remaining, available))
		released := canonicalQty(remaining - reactivated)
		status := "released"
		if reactivated > 0 {
			status = "active"
		}
		if _, err := tx.Exec(`
			INSERT INTO inventory_reservations
				(order_id, order_item_id, item_type, item_id, quantity, released_quantity, status)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, orderID, item.orderItemID, InventoryItemProduct, item.productID, remaining, released, status); err != nil {
			return err
		}
	}
	return nil
}

func loadLegacyShortages(itemType string) (map[int]float64, error) {
	result := make(map[int]float64)
	rows, err := DB.Query(`
		SELECT item_id, SUM(quantity)
		FROM stock_movements
		WHERE item_type = ? AND movement_type = ?
		GROUP BY item_id
	`, itemType, MovementLegacyShortage)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var itemID int
		var quantity float64
		if err := rows.Scan(&itemID, &quantity); err != nil {
			return nil, err
		}
		result[itemID] = quantity
	}
	return result, rows.Err()
}

// GetPurchaseDemand 按“实际库存-占用库存”计算原材料净需求和建议采购量。
func GetPurchaseDemand() ([]PurchaseDemandItem, error) {
	productionNeed := make(map[int]float64)
	legacyProductShortages, err := loadLegacyShortages(InventoryItemProduct)
	if err != nil {
		return nil, err
	}
	productRows, err := DB.Query(`
		SELECT p.id, p.stock,
		       COALESCE(r.reserved, 0) AS reserved,
		       COALESCE(d.demand, 0) AS demand
		FROM products p
		LEFT JOIN (
			SELECT item_id, SUM(quantity - consumed_quantity - released_quantity) AS reserved
			FROM inventory_reservations
			WHERE item_type = 'product' AND status = 'active'
			GROUP BY item_id
		) r ON r.item_id = p.id
		LEFT JOIN (
			SELECT oi.product_id,
			       SUM(GREATEST(oi.quantity - COALESCE(shipped.shipped_qty, 0), 0)) AS demand
			FROM order_items oi
			JOIN orders o ON o.id = oi.order_id
			LEFT JOIN (
				SELECT order_item_id, SUM(quantity) AS shipped_qty
				FROM product_outbound_items
				WHERE order_item_id > 0
				GROUP BY order_item_id
			) shipped ON shipped.order_item_id = oi.id
			WHERE o.status IN (0, 1, 2)
			GROUP BY oi.product_id
		) d ON d.product_id = p.id
	`)
	if err != nil {
		return nil, err
	}
	for productRows.Next() {
		var productID int
		var stock, reserved, demand float64
		if err := productRows.Scan(&productID, &stock, &reserved, &demand); err != nil {
			productRows.Close()
			return nil, err
		}
		available := math.Max(stock-reserved, 0)
		unreservedDemand := math.Max(demand-reserved, 0)
		productionNeed[productID] = canonicalQty(math.Max(unreservedDemand-available, 0) + legacyProductShortages[productID])
	}
	if err := productRows.Close(); err != nil {
		return nil, err
	}

	rawDemand := make(map[int]float64)
	legacyRawShortages, err := loadLegacyShortages(InventoryItemRawMaterial)
	if err != nil {
		return nil, err
	}
	for rawID, quantity := range legacyRawShortages {
		rawDemand[rawID] += quantity
	}
	bomRows, err := DB.Query("SELECT product_id, raw_material_id, quantity FROM product_bom")
	if err != nil {
		return nil, err
	}
	for bomRows.Next() {
		var productID, rawID int
		var qty float64
		if err := bomRows.Scan(&productID, &rawID, &qty); err != nil {
			bomRows.Close()
			return nil, err
		}
		rawDemand[rawID] += productionNeed[productID] * qty
	}
	if err := bomRows.Close(); err != nil {
		return nil, err
	}

	inTransitByRawMaterial := make(map[int]float64)
	inTransitByName := make(map[string]float64)
	transitRows, err := DB.Query(`
		SELECT COALESCE(pm.raw_material_id, 0), pm.material_name, COALESCE(pm.spec, ''), SUM(pm.quantity)
		FROM purchase_materials pm
		JOIN purchase_orders po ON po.id = pm.purchase_order_id
		WHERE po.status = 0 AND pm.material_type = '原材料'
		GROUP BY pm.raw_material_id, pm.material_name, COALESCE(pm.spec, '')
	`)
	if err != nil {
		return nil, err
	}
	for transitRows.Next() {
		var rawMaterialID int
		var name, spec string
		var qty float64
		if err := transitRows.Scan(&rawMaterialID, &name, &spec, &qty); err != nil {
			transitRows.Close()
			return nil, err
		}
		if rawMaterialID > 0 {
			inTransitByRawMaterial[rawMaterialID] += qty
		} else {
			inTransitByName[name+"\x00"+spec] += qty
		}
	}
	if err := transitRows.Close(); err != nil {
		return nil, err
	}

	rows, err := DB.Query(`
		SELECT rm.id, rm.name, COALESCE(rm.spec, ''), COALESCE(rm.unit, ''),
		       rm.stock, rm.min_stock, rm.avg_cost,
		       COALESCE(r.reserved, 0) AS reserved
		FROM raw_materials rm
		LEFT JOIN (
			SELECT item_id, SUM(quantity - consumed_quantity - released_quantity) AS reserved
			FROM inventory_reservations
			WHERE item_type = 'raw_material' AND status = 'active'
			GROUP BY item_id
		) r ON r.item_id = rm.id
		ORDER BY rm.name ASC, rm.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []PurchaseDemandItem{}
	for rows.Next() {
		var item PurchaseDemandItem
		if err := rows.Scan(&item.RawMaterialID, &item.Name, &item.Spec, &item.Unit,
			&item.ActualStock, &item.SafetyStock, &item.AvgCost, &item.ReservedStock); err != nil {
			return nil, err
		}
		item.AvailableStock = canonicalQty(item.ActualStock - item.ReservedStock)
		item.ProductionDemand = canonicalQty(rawDemand[item.RawMaterialID])
		item.InTransitPurchase = canonicalQty(
			inTransitByRawMaterial[item.RawMaterialID] + inTransitByName[item.Name+"\x00"+item.Spec],
		)
		item.NetDemand = canonicalQty(item.AvailableStock + item.InTransitPurchase - item.ProductionDemand - item.SafetyStock)
		if item.NetDemand < 0 {
			item.SuggestedPurchase = canonicalQty(-item.NetDemand)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func scanStockMovement(scanner interface {
	Scan(dest ...interface{}) error
}) (*StockMovement, error) {
	var movement StockMovement
	var stockBefore, stockAfter sql.NullFloat64
	err := scanner.Scan(
		&movement.ID, &movement.ItemType, &movement.ItemID, &movement.MovementType,
		&movement.Quantity, &stockBefore, &stockAfter, &movement.BatchNo, &movement.BusinessDate,
		&movement.UnitCost, &movement.TotalCost,
		&movement.ReferenceType, &movement.ReferenceID, &movement.ReferenceNo,
		&movement.OrderItemID, &movement.ReversalOf, &movement.OccurredAt,
		&movement.CreatedByUserID, &movement.Remark, &movement.CreatedAt,
		&movement.ItemName, &movement.ItemSpec, &movement.ItemUnit, &movement.OperatorName,
	)
	if err != nil {
		return nil, err
	}
	if stockBefore.Valid {
		value := stockBefore.Float64
		movement.StockBefore = &value
	}
	if stockAfter.Valid {
		value := stockAfter.Float64
		movement.StockAfter = &value
	}
	return &movement, nil
}

// ListStockMovementsByReferenceTx 返回某业务单据下尚未冲回的净入库流水。
func ListStockMovementsByReferenceTx(tx *sql.Tx, referenceType string, referenceID int64, movementType string) ([]StockMovement, error) {
	// 更新采购单时先冲回旧入库、再重新入库，原始入库流水仍保留。
	rows, err := tx.Query(`
		SELECT sm.id, sm.item_type, sm.item_id, sm.movement_type,
		       sm.quantity + COALESCE((
		           SELECT SUM(reversal.quantity)
		           FROM stock_movements reversal
		           WHERE reversal.reversal_of = sm.id
		       ), 0) AS outstanding_quantity,
		       sm.unit_cost, sm.total_cost,
		       sm.reference_type, sm.reference_id, sm.reference_no, sm.order_item_id, sm.reversal_of,
		       sm.occurred_at, sm.created_by_user_id, sm.remark, sm.created_at
		FROM stock_movements sm
		WHERE sm.reference_type = ? AND sm.reference_id = ? AND sm.movement_type = ?
		  AND sm.quantity + COALESCE((
		      SELECT SUM(reversal.quantity)
		      FROM stock_movements reversal
		      WHERE reversal.reversal_of = sm.id
		  ), 0) > 0.0005
		ORDER BY sm.id ASC
		FOR UPDATE
	`, referenceType, referenceID, movementType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []StockMovement
	for rows.Next() {
		var movement StockMovement
		if err := rows.Scan(
			&movement.ID, &movement.ItemType, &movement.ItemID, &movement.MovementType, &movement.Quantity,
			&movement.UnitCost, &movement.TotalCost,
			&movement.ReferenceType, &movement.ReferenceID, &movement.ReferenceNo,
			&movement.OrderItemID, &movement.ReversalOf, &movement.OccurredAt,
			&movement.CreatedByUserID, &movement.Remark, &movement.CreatedAt,
		); err != nil {
			return nil, err
		}
		list = append(list, movement)
	}
	return list, rows.Err()
}

// ListStockMovementsFiltered 查询面向账务审计的库存流水，包含物料与经办人信息。
func ListStockMovementsFiltered(filter StockMovementFilter) ([]StockMovement, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	if filter.ItemType != "" {
		if _, err := inventoryTable(filter.ItemType); err != nil {
			return nil, err
		}
	}
	query := `
		SELECT sm.id, sm.item_type, sm.item_id, sm.movement_type,
		       sm.quantity, sm.stock_before, sm.stock_after, sm.batch_no,
		       COALESCE(DATE_FORMAT(sm.business_date, '%Y-%m-%d'), '') AS business_date,
		       sm.unit_cost, sm.total_cost,
		       sm.reference_type, sm.reference_id, sm.reference_no,
		       sm.order_item_id, sm.reversal_of, sm.occurred_at,
		       sm.created_by_user_id, sm.remark, sm.created_at,
		       COALESCE(p.name, rm.name, '') AS item_name,
		       COALESCE(p.spec, rm.spec, '') AS item_spec,
		       COALESCE(p.unit, rm.unit, '') AS item_unit,
		       COALESCE(NULLIF(u.display_name, ''), u.username, '') AS operator_name
		FROM stock_movements sm
		LEFT JOIN products p ON sm.item_type = 'product' AND p.id = sm.item_id
		LEFT JOIN raw_materials rm ON sm.item_type = 'raw_material' AND rm.id = sm.item_id
		LEFT JOIN users u ON u.id = sm.created_by_user_id
		WHERE 1 = 1`
	args := []interface{}{}
	if filter.StartDate != "" {
		query += " AND COALESCE(sm.business_date, DATE(sm.occurred_at)) >= ?"
		args = append(args, filter.StartDate)
	}
	if filter.EndDate != "" {
		query += " AND COALESCE(sm.business_date, DATE(sm.occurred_at)) <= ?"
		args = append(args, filter.EndDate)
	}
	if filter.ItemType != "" {
		query += " AND sm.item_type = ?"
		args = append(args, filter.ItemType)
	}
	if filter.ItemID > 0 {
		query += " AND sm.item_id = ?"
		args = append(args, filter.ItemID)
	}
	switch filter.Direction {
	case "in":
		query += " AND sm.quantity > 0"
	case "out":
		query += " AND sm.quantity < 0"
	}
	if filter.MovementType != "" {
		query += " AND sm.movement_type = ?"
		args = append(args, filter.MovementType)
	}
	if filter.ReferenceNo != "" {
		query += " AND (sm.reference_no LIKE ? OR sm.batch_no LIKE ?)"
		like := "%" + filter.ReferenceNo + "%"
		args = append(args, like, like)
	}
	if filter.Operator != "" {
		query += " AND COALESCE(NULLIF(u.display_name, ''), u.username, '') LIKE ?"
		args = append(args, "%"+filter.Operator+"%")
	}
	query += " ORDER BY sm.occurred_at DESC, sm.id DESC LIMIT ? OFFSET ?"
	args = append(args, limit, filter.Offset)
	rows, err := DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []StockMovement{}
	for rows.Next() {
		movement, err := scanStockMovement(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *movement)
	}
	return list, rows.Err()
}

// ListStockMovements 保留原有调用方式，内部转到扩展查询。
func ListStockMovements(limit int, itemType string, itemID int) ([]StockMovement, error) {
	return ListStockMovementsFiltered(StockMovementFilter{
		Limit: limit, ItemType: itemType, ItemID: itemID,
	})
}
func migrateInventoryFoundation() error {
	if _, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			migration_name VARCHAR(100) PRIMARY KEY,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
	`); err != nil {
		return err
	}
	var count int
	if err := DB.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE migration_name = 'inventory_foundation_v1'").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 历史负库存只保留为缺口调整流水，实际库存归零，不再进入资产负债表。
	type negativeRow struct {
		id    int
		stock float64
	}
	loadNegatives := func(query string) ([]negativeRow, error) {
		rows, err := tx.Query(query)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var items []negativeRow
		for rows.Next() {
			var item negativeRow
			if err := rows.Scan(&item.id, &item.stock); err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return items, rows.Err()
	}
	productNegatives, err := loadNegatives("SELECT id, stock FROM products WHERE stock < 0 FOR UPDATE")
	if err != nil {
		return err
	}
	for _, item := range productNegatives {
		delta := -item.stock
		if _, err := tx.Exec(`
			INSERT INTO stock_movements
				(item_type, item_id, movement_type, quantity, unit_cost, total_cost,
				 reference_type, reference_no, remark)
			VALUES ('product', ?, 'legacy_shortage', ?, 0, 0, 'legacy_negative_stock', '', ?)
		`, item.id, delta, fmt.Sprintf("历史负库存 %.3f 已转为净需求缺口，不再作为账面库存", item.stock)); err != nil {
			return err
		}
		if _, err := tx.Exec("UPDATE products SET stock = 0 WHERE id = ?", item.id); err != nil {
			return err
		}
	}
	rawNegatives, err := loadNegatives("SELECT id, stock FROM raw_materials WHERE stock < 0 FOR UPDATE")
	if err != nil {
		return err
	}
	for _, item := range rawNegatives {
		delta := -item.stock
		if _, err := tx.Exec(`
			INSERT INTO stock_movements
				(item_type, item_id, movement_type, quantity, unit_cost, total_cost,
				 reference_type, reference_no, remark)
			VALUES ('raw_material', ?, 'legacy_shortage', ?, 0, 0, 'legacy_negative_stock', '', ?)
		`, item.id, delta, fmt.Sprintf("历史负库存 %.3f 已转为净需求缺口，不再作为账面库存", item.stock)); err != nil {
			return err
		}
		if _, err := tx.Exec("UPDATE raw_materials SET stock = 0 WHERE id = ?", item.id); err != nil {
			return err
		}
	}

	if _, err := tx.Exec("UPDATE raw_materials SET avg_cost = price WHERE avg_cost = 0 AND price > 0"); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		UPDATE products p
		LEFT JOIN (
			SELECT pb.product_id,
			       SUM(pb.quantity * COALESCE(NULLIF(rm.avg_cost, 0), rm.price, 0)) AS bom_cost
			FROM product_bom pb
			LEFT JOIN raw_materials rm ON rm.id = pb.raw_material_id
			GROUP BY pb.product_id
		) b ON b.product_id = p.id
		SET p.avg_cost = COALESCE(b.bom_cost, 0)
		WHERE p.avg_cost = 0
	`); err != nil {
		return err
	}

	// 已发货订单在迁移时冻结历史成本，后续采购涨跌不再重算过去利润。
	if _, err := tx.Exec(`
		INSERT INTO stock_movements
			(item_type, item_id, movement_type, quantity, unit_cost, total_cost,
			 reference_type, reference_id, reference_no, order_item_id, reversal_of,
			 occurred_at, created_by_user_id, remark)
		SELECT 'product', oi.product_id, 'legacy_sales_cost', 0,
		       COALESCE(bom.unit_cost, 0),
		       -COALESCE(bom.unit_cost, 0) * oi.quantity,
		       'order_item', oi.id, o.order_no, oi.id, 0,
		       COALESCE(o.shipped_date, o.order_date, DATE(o.created_at)), 0,
		       '历史已发货订单成本在库存核算切换时一次性冻结'
		FROM orders o
		JOIN order_items oi ON oi.order_id = o.id
		LEFT JOIN (
			SELECT pb.product_id,
			       SUM(pb.quantity * COALESCE(NULLIF(rm.avg_cost, 0), rm.price, 0)) AS unit_cost
			FROM product_bom pb
			LEFT JOIN raw_materials rm ON rm.id = pb.raw_material_id
			GROUP BY pb.product_id
		) bom ON bom.product_id = oi.product_id
		WHERE o.status = 3
		  AND NOT EXISTS (
			SELECT 1
			FROM stock_movements sm
			WHERE sm.item_type = 'product'
			  AND sm.order_item_id = oi.id
			  AND sm.movement_type IN ('sales_out', 'sales_out_reversal', 'legacy_sales_cost')
		  )
	`); err != nil {
		return err
	}
	// 为迁移时的实际库存建立期初流水，后续流水之和与实际库存保持同源。
	if _, err := tx.Exec(`
		INSERT INTO stock_movements
			(item_type, item_id, movement_type, quantity, unit_cost, total_cost,
			 reference_type, reference_no, remark)
		SELECT 'product', p.id, 'opening', p.stock, p.avg_cost, p.stock * p.avg_cost,
		       'migration', '', '库存核算切换期初余额'
		FROM products p
		WHERE p.stock <> 0
		  AND NOT EXISTS (
			SELECT 1 FROM stock_movements sm
			WHERE sm.item_type = 'product' AND sm.item_id = p.id AND sm.movement_type = 'opening'
		  )
	`); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO stock_movements
			(item_type, item_id, movement_type, quantity, unit_cost, total_cost,
			 reference_type, reference_no, remark)
		SELECT 'raw_material', rm.id, 'opening', rm.stock, rm.avg_cost, rm.stock * rm.avg_cost,
		       'migration', '', '库存核算切换期初余额'
		FROM raw_materials rm
		WHERE rm.stock <> 0
		  AND NOT EXISTS (
			SELECT 1 FROM stock_movements sm
			WHERE sm.item_type = 'raw_material' AND sm.item_id = rm.id AND sm.movement_type = 'opening'
		  )
	`); err != nil {
		return err
	}

	if _, err := tx.Exec("INSERT INTO schema_migrations (migration_name) VALUES ('inventory_foundation_v1')"); err != nil {
		return err
	}
	return tx.Commit()
}
