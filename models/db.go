package models

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/go-sql-driver/mysql"
)

var DB *sql.DB

func InitDB(dataSourceName string) {
	var err error
	DB, err = sql.Open("mysql", dataSourceName)
	if err != nil {
		log.Fatal(err)
	}
	if err = DB.Ping(); err != nil {
		log.Fatal(err)
	}
	if err = EnsureUsersTable(); err != nil {
		log.Fatal(err)
	}
	if err = EnsureUserPermissions(); err != nil {
		log.Fatal(err)
	}
	if err = ensureProductAndMaterialColumns(); err != nil {
		log.Fatal(err)
	}
	if err = EnsurePurchaseMaterialsTable(); err != nil {
		log.Fatal(err)
	}
	if err = EnsurePurchaseOrdersTable(); err != nil {
		log.Fatal(err)
	}
	if err = EnsureCustomersTable(); err != nil {
		log.Fatal(err)
	}
	if err = EnsureCustomerColumns(); err != nil {
		log.Fatal(err)
	}
	if err = EnsureCustomerContactsTable(); err != nil {
		log.Fatal(err)
	}
	if err = EnsureCustomerAttachmentsTable(); err != nil {
		log.Fatal(err)
	}
	if err = EnsureProductOutboundTables(); err != nil {
		log.Fatal(err)
	}
	if err = ensureOrderColumns(); err != nil {
		log.Fatal(err)
	}
	if err = EnsureOrderBOMSnapshotTable(); err != nil {
		log.Fatal(err)
	}
	log.Println("Database connected")
}

// ensureColumn 检查当前数据库的指定表是否包含指定列，若不存在则添加。
func ensureColumn(table, column, definition string) error {
	var count int
	err := DB.QueryRow(`
        SELECT COUNT(*)
        FROM information_schema.COLUMNS
        WHERE TABLE_SCHEMA = DATABASE()
          AND TABLE_NAME = ?
          AND COLUMN_NAME = ?
    `, table, column).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	_, err = DB.Exec(fmt.Sprintf("ALTER TABLE `%s` ADD COLUMN `%s` %s", table, column, definition))
	return err
}

// ensureIndex 若指定普通索引不存在则创建。
func ensureIndex(table, indexName, columns string) error {
	var count int
	err := DB.QueryRow(`
        SELECT COUNT(*)
        FROM information_schema.STATISTICS
        WHERE TABLE_SCHEMA = DATABASE()
          AND TABLE_NAME = ?
          AND INDEX_NAME = ?
    `, table, indexName).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	_, err = DB.Exec(fmt.Sprintf("CREATE INDEX `%s` ON `%s` (%s)", indexName, table, columns))
	return err
}

// ensureUniqueIndex 若指定唯一索引不存在则创建。
func ensureUniqueIndex(table, indexName, column string) error {
	var count int
	err := DB.QueryRow(`
        SELECT COUNT(*)
        FROM information_schema.STATISTICS
        WHERE TABLE_SCHEMA = DATABASE()
          AND TABLE_NAME = ?
          AND INDEX_NAME = ?
    `, table, indexName).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	_, err = DB.Exec(fmt.Sprintf("CREATE UNIQUE INDEX `%s` ON `%s` (`%s`)", indexName, table, column))
	return err
}

// ensureProductAndMaterialColumns 为成品和原材料表补充规格型号、单位等字段。
func ensureProductAndMaterialColumns() error {
	if err := ensureColumn("products", "spec", "VARCHAR(100) NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureColumn("products", "unit", "VARCHAR(20) NOT NULL DEFAULT '个'"); err != nil {
		return err
	}
	if err := ensureColumn("products", "packaging", "VARCHAR(50) NOT NULL DEFAULT '' COMMENT '包装方式'"); err != nil {
		return err
	}
	if err := ensureColumn("raw_materials", "images", "TEXT"); err != nil {
		return err
	}
	if err := ensureColumn("raw_materials", "spec", "VARCHAR(100) NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	return nil
}

// ensureOrderColumns 为订单表补充重新设计后的字段，并迁移旧状态值。
func ensureOrderColumns() error {
	if err := ensureColumn("orders", "region", "VARCHAR(100) NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureColumn("orders", "order_date", "DATE NULL"); err != nil {
		return err
	}
	if err := ensureColumn("orders", "expected_shipping_date", "DATE NULL"); err != nil {
		return err
	}
	if err := ensureColumn("orders", "customer_required_date", "DATE NULL"); err != nil {
		return err
	}
	if err := ensureColumn("orders", "logistics_days", "INT NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureColumn("orders", "created_by_user_id", "INT NULL"); err != nil {
		return err
	}
	if err := ensureColumn("orders", "shipped_date", "DATE NULL"); err != nil {
		return err
	}
	if err := ensureColumn("orders", "payment_status", "TINYINT NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureColumn("orders", "prepared_by", "VARCHAR(100) NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureColumn("orders", "payment_settlement", "VARCHAR(50) NOT NULL DEFAULT '现付'"); err != nil {
		return err
	}
	if err := ensureColumn("orders", "freight_payment", "VARCHAR(50) NOT NULL DEFAULT '现付'"); err != nil {
		return err
	}
	if err := ensureColumn("orders", "freight_recovery", "VARCHAR(20) NOT NULL DEFAULT '可回收'"); err != nil {
		return err
	}
	if err := ensureColumn("orders", "transport_method", "VARCHAR(50) NOT NULL DEFAULT '物流'"); err != nil {
		return err
	}
	if err := ensureColumn("orders", "customer_id", "INT NOT NULL DEFAULT 0 COMMENT '关联客户ID'"); err != nil {
		return err
	}
	if err := ensureColumn("orders", "owner_user_id", "INT NOT NULL DEFAULT 0 COMMENT '负责人/业务员用户ID'"); err != nil {
		return err
	}
	if err := ensureColumn("orders", "currency", "VARCHAR(20) NOT NULL DEFAULT 'CNY' COMMENT '币种'"); err != nil {
		return err
	}
	if err := ensureColumn("orders", "trade_terms", "VARCHAR(20) NOT NULL DEFAULT '' COMMENT '贸易术语'"); err != nil {
		return err
	}
	if err := ensureColumn("orders", "shipping_mark", "VARCHAR(1000) NOT NULL DEFAULT '' COMMENT '唛头'"); err != nil {
		return err
	} // 历史订单空值回填为默认值（早期版本可能写入空字符串）
	if _, err := DB.Exec("UPDATE orders SET payment_settlement = '现付' WHERE payment_settlement IS NULL OR payment_settlement = ''"); err != nil {
		return err
	}
	if _, err := DB.Exec("UPDATE orders SET freight_payment = '现付' WHERE freight_payment IS NULL OR freight_payment = ''"); err != nil {
		return err
	}
	if _, err := DB.Exec("UPDATE orders SET freight_recovery = '可回收' WHERE freight_recovery IS NULL OR freight_recovery = ''"); err != nil {
		return err
	}
	if _, err := DB.Exec("UPDATE orders SET transport_method = '物流' WHERE transport_method IS NULL OR transport_method = ''"); err != nil {
		return err
	}

	// 旧字段 delivery_date 作为历史数据回填到预计发货日期。
	if _, err := DB.Exec("UPDATE orders SET expected_shipping_date = delivery_date WHERE expected_shipping_date IS NULL AND delivery_date IS NOT NULL"); err != nil {
		return err
	}
	// 下单日期为空时，使用创建时间日期回填。
	if _, err := DB.Exec("UPDATE orders SET order_date = DATE(created_at) WHERE order_date IS NULL"); err != nil {
		return err
	}

	if err := backfillOrderCreatedBy(); err != nil {
		return err
	}
	if err := backfillShippedDate(); err != nil {
		return err
	}
	return migrateOrderStatusV2()
}

// migrateOrderStatusV2 将旧订单状态（0待处理/1已完成/2已取消）迁移为新状态。
func migrateOrderStatusV2() error {
	_, err := DB.Exec(`
        CREATE TABLE IF NOT EXISTS schema_migrations (
            migration_name VARCHAR(100) PRIMARY KEY,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
    `)
	if err != nil {
		return err
	}

	var count int
	err = DB.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE migration_name = 'order_status_v2'").Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	if _, err := DB.Exec("UPDATE orders SET status = 3 WHERE status = 1"); err != nil {
		return err
	}
	if _, err := DB.Exec("UPDATE orders SET status = 4 WHERE status = 2"); err != nil {
		return err
	}
	if _, err := DB.Exec("INSERT IGNORE INTO schema_migrations (migration_name) VALUES ('order_status_v2')"); err != nil {
		return err
	}
	return nil
}

// backfillOrderCreatedBy 按制单人姓名/用户名回填订单创建人（created_by_user_id），一次性执行。
func backfillOrderCreatedBy() error {
	var count int
	if err := DB.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE migration_name = 'order_created_by_backfill'").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	_, err := DB.Exec(`
        UPDATE orders o
        LEFT JOIN users u ON u.username = o.prepared_by OR (u.display_name <> '' AND u.display_name = o.prepared_by)
        SET o.created_by_user_id = u.id
        WHERE o.created_by_user_id IS NULL
    `)
	if err != nil {
		return err
	}
	if _, err := DB.Exec("INSERT IGNORE INTO schema_migrations (migration_name) VALUES ('order_created_by_backfill')"); err != nil {
		return err
	}
	return nil
}

// backfillShippedDate 为历史已发货订单回填实际发货日期（按 预计发货日/下单日/创建日 顺序），一次性执行。
func backfillShippedDate() error {
	var count int
	if err := DB.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE migration_name = 'order_shipped_date_backfill'").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if _, err := DB.Exec(`
        UPDATE orders
        SET shipped_date = COALESCE(expected_shipping_date, order_date, DATE(created_at))
        WHERE status = 3 AND shipped_date IS NULL
    `); err != nil {
		return err
	}
	if _, err := DB.Exec("INSERT IGNORE INTO schema_migrations (migration_name) VALUES ('order_shipped_date_backfill')"); err != nil {
		return err
	}
	return nil
}
