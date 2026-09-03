package models

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ============ 客户档案：基础选项（前端下拉选项与后端校验共用语义） ============
var CustomerTypes = []string{
	"国内OEM", "国内品牌经销商", "海外OEM", "海外品牌商",
	"外贸贸易商", "代理商", "终端客户",
}
var CustomerStatuses = []string{"潜在客户", "意向客户", "正式合作", "暂停合作", "终止合作"}
var CustomerLevels = []string{"A", "B", "C", "D"}
var CooperationModes = []string{"OEM贴牌", "ODM定制配方", "采购自有品牌", "定制包材", "散装大桶墨水采购", "大品牌分销商"}
var TargetMarkets = []string{"国内", "东南亚", "欧洲", "美洲", "中东", "其他"}
var Currencies = []string{"CNY", "USD", "EUR", "GBP", "JPY", "HKD", "AUD"}
var PaymentTerms = []string{"现结", "到付", "月结30天", "月结60天", "T/T", "L/C", "其他"}
var TradeTerms = []string{"EXW", "FOB", "CIF", "DDP"}
var Sources = []string{"展会", "阿里国际站", "独立站", "转介绍", "其他"}
var RiskLevels = []string{"低", "中", "高"}
var ContactTitles = []string{"老板", "总监", "业务", "采购", "技术"}

// exportCustomerTypes 外贸/海外型客户：编号前缀使用 KHFW（按确认规则，外贸贸易商归入海外）。
var exportCustomerTypes = map[string]bool{
	"海外OEM": true, "海外品牌商": true, "外贸贸易商": true,
}

// foreignCustomerTypes 境外公司（需要填写国家/地区）。
var foreignCustomerTypes = map[string]bool{
	"海外OEM": true, "海外品牌商": true,
}

// oemCustomerTypes OEM 型客户（品牌必填）。
var oemCustomerTypes = map[string]bool{
	"国内OEM": true, "海外OEM": true,
}

// IsExportCustomerType 是否按海外/外贸编码（KHFW）
func IsExportCustomerType(t string) bool { return exportCustomerTypes[t] }

// IsForeignCustomerType 是否为境外公司（需填国家/地区）
func IsForeignCustomerType(t string) bool { return foreignCustomerTypes[t] }

// IsOEMCustomerType 是否为 OEM 型客户（品牌必填）
func IsOEMCustomerType(t string) bool { return oemCustomerTypes[t] }

// JoinCSV 将字符串切片以逗号连接（用于多选字段落库）。
func JoinCSV(items []string) string { return strings.Join(items, ",") }

// SplitCSV 将逗号分隔的字符串还原为切片。
func SplitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ============ 客户主表 ============
type Customer struct {
	ID               int       `json:"id"`
	Name             string    `json:"name"`
	Phone            string    `json:"phone"`
	Address          string    `json:"address"`
	Code             string    `json:"code"`
	FullName         string    `json:"full_name"`
	CustomerType     string    `json:"customer_type"`
	Status           string    `json:"status"`
	Level            string    `json:"level"`
	CooperationModes []string  `json:"cooperation_modes"`
	BrandNames       []string  `json:"brand_names"`
	TargetMarkets    []string  `json:"target_markets"`
	Currency         string    `json:"currency"`
	PaymentTerms     string    `json:"payment_terms"`
	TradeTerms       string    `json:"trade_terms"`
	CountryRegion    string    `json:"country_region"`
	InvoiceInfo      string    `json:"invoice_info"`
	ShippingMark     string    `json:"shipping_mark"`
	Source           string    `json:"source"`
	OwnerUserID      int       `json:"owner_user_id"`
	OwnerName        string    `json:"owner_name"`
	RiskLevel        string    `json:"risk_level"`
	Remark           string    `json:"remark"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	CreatedByUserID  int       `json:"created_by_user_id"`
	UpdatedByUserID  int       `json:"updated_by_user_id"`
}

const customerSelectCols = `c.id, c.name, c.phone, c.address, c.code, c.full_name, c.customer_type,
	c.status, c.level, c.cooperation_modes, c.brand_names, c.target_markets,
	c.currency, c.payment_terms, c.trade_terms, c.country_region, c.invoice_info,
	c.shipping_mark, c.source, c.owner_user_id, c.risk_level, COALESCE(c.remark, ''),
	c.created_at, COALESCE(c.updated_at, c.created_at),
	c.created_by_user_id, c.updated_by_user_id,
	COALESCE(NULLIF(u.display_name, ''), u.username, '') AS owner_name`

type customerScanner interface {
	Scan(dest ...any) error
}

func scanCustomer(s customerScanner) (*Customer, error) {
	var c Customer
	var modes, brands, markets string
	err := s.Scan(
		&c.ID, &c.Name, &c.Phone, &c.Address, &c.Code, &c.FullName, &c.CustomerType,
		&c.Status, &c.Level, &modes, &brands, &markets,
		&c.Currency, &c.PaymentTerms, &c.TradeTerms, &c.CountryRegion, &c.InvoiceInfo,
		&c.ShippingMark, &c.Source, &c.OwnerUserID, &c.RiskLevel, &c.Remark,
		&c.CreatedAt, &c.UpdatedAt, &c.CreatedByUserID, &c.UpdatedByUserID, &c.OwnerName,
	)
	if err != nil {
		return nil, err
	}
	c.CooperationModes = SplitCSV(modes)
	c.BrandNames = SplitCSV(brands)
	c.TargetMarkets = SplitCSV(markets)
	return &c, nil
}

// EnsureCustomersTable 创建客户表（基础字段，其余字段由 EnsureCustomerColumns 迁移补齐）。
func EnsureCustomersTable() error {
	_, err := DB.Exec(`
        CREATE TABLE IF NOT EXISTS customers (
            id          INT AUTO_INCREMENT PRIMARY KEY COMMENT '客户ID',
            name        VARCHAR(100) NOT NULL COMMENT '客户名称',
            phone       VARCHAR(20)  NOT NULL DEFAULT '' COMMENT '电话号码',
            address     VARCHAR(255) NOT NULL DEFAULT '' COMMENT '收货地址',
            created_at  DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间'
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='客户表'
    `)
	return err
}

// EnsureCustomerColumns 为 customers 表补充客户档案字段，并为历史数据回填编号、建唯一索引。
func EnsureCustomerColumns() error {
	cols := []struct{ name, def string }{
		{"code", "VARCHAR(32) NOT NULL DEFAULT '' COMMENT '客户编号'"},
		{"full_name", "VARCHAR(255) NOT NULL DEFAULT '' COMMENT '客户全称'"},
		{"customer_type", "VARCHAR(50) NOT NULL DEFAULT '' COMMENT '客户类型'"},
		{"status", "VARCHAR(30) NOT NULL DEFAULT '潜在客户' COMMENT '客户状态'"},
		{"level", "VARCHAR(5) NOT NULL DEFAULT '' COMMENT '客户等级A/B/C/D'"},
		{"cooperation_modes", "VARCHAR(255) NOT NULL DEFAULT '' COMMENT '合作模式(逗号分隔)'"},
		{"brand_names", "VARCHAR(500) NOT NULL DEFAULT '' COMMENT '客户品牌(逗号分隔)'"},
		{"target_markets", "VARCHAR(255) NOT NULL DEFAULT '' COMMENT '目标市场(逗号分隔)'"},
		{"currency", "VARCHAR(20) NOT NULL DEFAULT 'CNY' COMMENT '客户币种'"},
		{"payment_terms", "VARCHAR(50) NOT NULL DEFAULT '' COMMENT '结算方式'"},
		{"trade_terms", "VARCHAR(20) NOT NULL DEFAULT '' COMMENT '贸易术语'"},
		{"country_region", "VARCHAR(100) NOT NULL DEFAULT '' COMMENT '国家/地区'"},
		{"invoice_info", "VARCHAR(1000) NOT NULL DEFAULT '' COMMENT '开票信息'"},
		{"shipping_mark", "VARCHAR(1000) NOT NULL DEFAULT '' COMMENT '唛头模板文本'"},
		{"source", "VARCHAR(50) NOT NULL DEFAULT '' COMMENT '客户来源'"},
		{"owner_user_id", "INT NOT NULL DEFAULT 0 COMMENT '所属业务员用户ID'"},
		{"risk_level", "VARCHAR(10) NOT NULL DEFAULT '低' COMMENT '风险等级'"},
		{"remark", "TEXT COMMENT '备注'"},
		{"created_by_user_id", "INT NOT NULL DEFAULT 0 COMMENT '创建人用户ID'"},
		{"updated_by_user_id", "INT NOT NULL DEFAULT 0 COMMENT '更新人用户ID'"},
		{"updated_at", "DATETIME NULL COMMENT '更新时间'"},
	}
	for _, col := range cols {
		if err := ensureColumn("customers", col.name, col.def); err != nil {
			return err
		}
	}
	if err := backfillCustomerCodes(); err != nil {
		return err
	}
	return ensureUniqueIndex("customers", "uk_customers_code", "code")
}

// backfillCustomerCodes 为历史客户回填编号（按国内 KHCN + id 序号）。
func backfillCustomerCodes() error {
	rows, err := DB.Query("SELECT id FROM customers WHERE code = '' OR code IS NULL ORDER BY id")
	if err != nil {
		return err
	}
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		code, err := NextCustomerCode("")
		if err != nil {
			return err
		}
		if _, err := DB.Exec("UPDATE customers SET code = ? WHERE id = ?", code, id); err != nil {
			return err
		}
	}
	return nil
}

// NextCustomerCode 生成下一个客户编号：KH + CN/FW + 4 位流水号。
func NextCustomerCode(customerType string) (string, error) {
	prefix := "KHCN"
	if IsExportCustomerType(customerType) {
		prefix = "KHFW"
	}
	var code string
	err := DB.QueryRow("SELECT code FROM customers WHERE code LIKE ? ORDER BY code DESC LIMIT 1", prefix+"%").Scan(&code)
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}
	maxSeq := 0
	if err == nil {
		numStr := strings.TrimPrefix(code, prefix)
		if n, perr := strconv.Atoi(numStr); perr == nil && n > maxSeq {
			maxSeq = n
		}
	}
	return fmt.Sprintf("%s%04d", prefix, maxSeq+1), nil
}

func isDuplicateKeyErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Duplicate entry")
}

// GetAllCustomers 获取客户列表（含所属业务员名称）。
func GetAllCustomers() ([]Customer, error) {
	rows, err := DB.Query(`SELECT ` + customerSelectCols + `
        FROM customers c
        LEFT JOIN users u ON u.id = c.owner_user_id
        ORDER BY c.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Customer
	for rows.Next() {
		c, err := scanCustomer(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *c)
	}
	return list, rows.Err()
}

// GetCustomerByID 获取单个客户。
func GetCustomerByID(id int) (*Customer, error) {
	row := DB.QueryRow(`SELECT `+customerSelectCols+`
        FROM customers c
        LEFT JOIN users u ON u.id = c.owner_user_id
        WHERE c.id = ?`, id)
	return scanCustomer(row)
}

// AddCustomer 新增客户；编号为空时按规则自动生成（含并发冲突重试）。返回 id 与最终编号。
func AddCustomer(c Customer, createdByUserID int) (int64, string, error) {
	for attempt := 0; attempt < 10; attempt++ {
		code := strings.TrimSpace(c.Code)
		if code == "" {
			generated, err := NextCustomerCode(c.CustomerType)
			if err != nil {
				return 0, "", err
			}
			code = generated
		}
		res, err := DB.Exec(`
            INSERT INTO customers
                (name, phone, address, code, full_name, customer_type, status, level,
                 cooperation_modes, brand_names, target_markets, currency, payment_terms,
                 trade_terms, country_region, invoice_info, shipping_mark, source,
                 owner_user_id, risk_level, remark, created_by_user_id, updated_by_user_id)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			c.Name, c.Phone, c.Address, code, c.FullName, c.CustomerType, c.Status, c.Level,
			JoinCSV(c.CooperationModes), JoinCSV(c.BrandNames), JoinCSV(c.TargetMarkets),
			c.Currency, c.PaymentTerms, c.TradeTerms, c.CountryRegion, c.InvoiceInfo,
			c.ShippingMark, c.Source, c.OwnerUserID, c.RiskLevel, c.Remark,
			createdByUserID, createdByUserID)
		if err == nil {
			id, _ := res.LastInsertId()
			return id, code, nil
		}
		if isDuplicateKeyErr(err) && strings.TrimSpace(c.Code) == "" {
			continue // 并发撞号：重新生成
		}
		return 0, "", err
	}
	return 0, "", fmt.Errorf("生成客户编号失败，请重试")
}

// UpdateCustomer 更新客户（编号与创建信息不可修改）。
func UpdateCustomer(c Customer, updatedByUserID int) error {
	_, err := DB.Exec(`
        UPDATE customers SET
            name = ?, phone = ?, address = ?, full_name = ?, customer_type = ?,
            status = ?, level = ?, cooperation_modes = ?, brand_names = ?,
            target_markets = ?, currency = ?, payment_terms = ?, trade_terms = ?,
            country_region = ?, invoice_info = ?, shipping_mark = ?, source = ?,
            owner_user_id = ?, risk_level = ?, remark = ?,
            updated_by_user_id = ?, updated_at = NOW()
        WHERE id = ?`,
		c.Name, c.Phone, c.Address, c.FullName, c.CustomerType,
		c.Status, c.Level, JoinCSV(c.CooperationModes), JoinCSV(c.BrandNames),
		JoinCSV(c.TargetMarkets), c.Currency, c.PaymentTerms, c.TradeTerms,
		c.CountryRegion, c.InvoiceInfo, c.ShippingMark, c.Source,
		c.OwnerUserID, c.RiskLevel, c.Remark,
		updatedByUserID, c.ID)
	return err
}

// CountOrdersByCustomer 统计关联该客户的订单数。
func CountOrdersByCustomer(customerID int) (int, error) {
	var n int
	err := DB.QueryRow("SELECT COUNT(*) FROM orders WHERE customer_id = ?", customerID).Scan(&n)
	return n, err
}

// DeleteCustomer 删除客户（同时清理其联系人）。
func DeleteCustomer(id int) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DELETE FROM customer_contacts WHERE customer_id = ?", id); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM customer_attachments WHERE customer_id = ?", id); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM customers WHERE id = ?", id); err != nil {
		return err
	}
	return tx.Commit()
}

// ============ 客户联系人 ============
type CustomerContact struct {
	ID            int       `json:"id"`
	CustomerID    int       `json:"customer_id"`
	Name          string    `json:"name"`
	Title         string    `json:"title"`
	Mobile        string    `json:"mobile"`
	Phone         string    `json:"phone"`
	Email         string    `json:"email"`
	Wechat        string    `json:"wechat"`
	Whatsapp      string    `json:"whatsapp"`
	SocialAccount string    `json:"social_account"`
	IsPrimary     bool      `json:"is_primary"`
	Remark        string    `json:"remark"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// EnsureCustomerContactsTable 创建客户联系人表。
func EnsureCustomerContactsTable() error {
	_, err := DB.Exec(`
        CREATE TABLE IF NOT EXISTS customer_contacts (
            id             INT AUTO_INCREMENT PRIMARY KEY COMMENT '联系人ID',
            customer_id    INT NOT NULL COMMENT '客户ID',
            name           VARCHAR(100) NOT NULL COMMENT '联系人姓名',
            title          VARCHAR(50)  NOT NULL DEFAULT '' COMMENT '职位',
            mobile         VARCHAR(30)  NOT NULL DEFAULT '' COMMENT '手机',
            phone          VARCHAR(30)  NOT NULL DEFAULT '' COMMENT '电话',
            email          VARCHAR(100) NOT NULL DEFAULT '' COMMENT '邮箱',
            wechat         VARCHAR(100) NOT NULL DEFAULT '' COMMENT '微信',
            whatsapp       VARCHAR(100) NOT NULL DEFAULT '' COMMENT 'WhatsApp',
            social_account VARCHAR(255) NOT NULL DEFAULT '' COMMENT '社交账号',
            is_primary     TINYINT      NOT NULL DEFAULT 0 COMMENT '是否主联系人',
            remark         VARCHAR(500) NOT NULL DEFAULT '' COMMENT '备注',
            created_at     DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
            updated_at     DATETIME NULL COMMENT '更新时间',
            KEY idx_customer (customer_id)
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='客户联系人表'
    `)
	return err
}

// ListCustomerContacts 获取某客户的全部联系人。
func ListCustomerContacts(customerID int) ([]CustomerContact, error) {
	rows, err := DB.Query(`
        SELECT id, customer_id, name, title, mobile, phone, email, wechat, whatsapp,
               social_account, is_primary, remark, created_at, COALESCE(updated_at, created_at)
        FROM customer_contacts
        WHERE customer_id = ?
        ORDER BY is_primary DESC, id ASC`, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []CustomerContact
	for rows.Next() {
		var ct CustomerContact
		var primary int
		if err := rows.Scan(&ct.ID, &ct.CustomerID, &ct.Name, &ct.Title, &ct.Mobile,
			&ct.Phone, &ct.Email, &ct.Wechat, &ct.Whatsapp, &ct.SocialAccount,
			&primary, &ct.Remark, &ct.CreatedAt, &ct.UpdatedAt); err != nil {
			return nil, err
		}
		ct.IsPrimary = primary == 1
		list = append(list, ct)
	}
	return list, rows.Err()
}

// GetCustomerContactByID 获取单个联系人。
func GetCustomerContactByID(id int) (*CustomerContact, error) {
	var ct CustomerContact
	var primary int
	err := DB.QueryRow(`
        SELECT id, customer_id, name, title, mobile, phone, email, wechat, whatsapp,
               social_account, is_primary, remark, created_at, COALESCE(updated_at, created_at)
        FROM customer_contacts WHERE id = ?`, id).
		Scan(&ct.ID, &ct.CustomerID, &ct.Name, &ct.Title, &ct.Mobile,
			&ct.Phone, &ct.Email, &ct.Wechat, &ct.Whatsapp, &ct.SocialAccount,
			&primary, &ct.Remark, &ct.CreatedAt, &ct.UpdatedAt)
	if err != nil {
		return nil, err
	}
	ct.IsPrimary = primary == 1
	return &ct, nil
}

// AddCustomerContact 新增联系人；若标记为主联系人则先取消同客户其他主联系人。
func AddCustomerContact(ct CustomerContact) (int64, error) {
	tx, err := DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	if ct.IsPrimary {
		if _, err := tx.Exec("UPDATE customer_contacts SET is_primary = 0 WHERE customer_id = ?", ct.CustomerID); err != nil {
			return 0, err
		}
	}
	primary := 0
	if ct.IsPrimary {
		primary = 1
	}
	res, err := tx.Exec(`
        INSERT INTO customer_contacts
            (customer_id, name, title, mobile, phone, email, wechat, whatsapp,
             social_account, is_primary, remark)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ct.CustomerID, ct.Name, ct.Title, ct.Mobile, ct.Phone, ct.Email,
		ct.Wechat, ct.Whatsapp, ct.SocialAccount, primary, ct.Remark)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateCustomerContact 更新联系人；若标记为主联系人则先取消同客户其他主联系人。
func UpdateCustomerContact(ct CustomerContact) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if ct.IsPrimary {
		if _, err := tx.Exec("UPDATE customer_contacts SET is_primary = 0 WHERE customer_id = ? AND id <> ?", ct.CustomerID, ct.ID); err != nil {
			return err
		}
	}
	primary := 0
	if ct.IsPrimary {
		primary = 1
	}
	if _, err := tx.Exec(`
        UPDATE customer_contacts SET
            name = ?, title = ?, mobile = ?, phone = ?, email = ?, wechat = ?,
            whatsapp = ?, social_account = ?, is_primary = ?, remark = ?,
            updated_at = NOW()
        WHERE id = ?`,
		ct.Name, ct.Title, ct.Mobile, ct.Phone, ct.Email, ct.Wechat,
		ct.Whatsapp, ct.SocialAccount, primary, ct.Remark, ct.ID); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteCustomerContact 删除联系人。
func DeleteCustomerContact(id int) error {
	_, err := DB.Exec("DELETE FROM customer_contacts WHERE id = ?", id)
	return err
}

// GetAllCustomersOwnedBy 获取当前用户可见的客户：本人负责 + 本人创建 + 历史公共客户（无负责人且无创建人）。
func GetAllCustomersOwnedBy(userID int) ([]Customer, error) {
	rows, err := DB.Query(`SELECT `+customerSelectCols+`
        FROM customers c
        LEFT JOIN users u ON u.id = c.owner_user_id
        WHERE c.owner_user_id = ? OR c.created_by_user_id = ? OR (c.owner_user_id = 0 AND c.created_by_user_id = 0)
        ORDER BY c.id DESC`, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Customer
	for rows.Next() {
		c, err := scanCustomer(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *c)
	}
	return list, rows.Err()
}

// ============ 客户附件 ============
type CustomerAttachment struct {
	ID               int       `json:"id"`
	CustomerID       int       `json:"customer_id"`
	Category         string    `json:"category"`
	FileName         string    `json:"file_name"`
	StoredName       string    `json:"stored_name"`
	FileSize         int64     `json:"file_size"`
	Mime             string    `json:"mime"`
	URL              string    `json:"url"`
	UploadedByUserID int       `json:"uploaded_by_user_id"`
	UploadedByName   string    `json:"uploaded_by_name"`
	CreatedAt        time.Time `json:"created_at"`
}

// EnsureCustomerAttachmentsTable 创建客户附件表。
func EnsureCustomerAttachmentsTable() error {
	_, err := DB.Exec(`
        CREATE TABLE IF NOT EXISTS customer_attachments (
            id                  INT AUTO_INCREMENT PRIMARY KEY COMMENT '附件ID',
            customer_id         INT NOT NULL COMMENT '客户ID',
            category            VARCHAR(50)  NOT NULL DEFAULT '其他' COMMENT '附件分类',
            file_name           VARCHAR(255) NOT NULL DEFAULT '' COMMENT '原始文件名',
            stored_name         VARCHAR(255) NOT NULL DEFAULT '' COMMENT '存储文件名',
            file_size           BIGINT NOT NULL DEFAULT 0 COMMENT '文件大小(字节)',
            mime                VARCHAR(100) NOT NULL DEFAULT '' COMMENT 'MIME类型',
            uploaded_by_user_id INT NOT NULL DEFAULT 0 COMMENT '上传人用户ID',
            created_at          DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '上传时间',
            KEY idx_customer (customer_id)
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='客户附件表'
    `)
	return err
}

// ListCustomerAttachments 获取某客户的全部附件。
func ListCustomerAttachments(customerID int) ([]CustomerAttachment, error) {
	rows, err := DB.Query(`
        SELECT a.id, a.customer_id, a.category, a.file_name, a.stored_name, a.file_size,
               a.mime, a.uploaded_by_user_id, COALESCE(NULLIF(u.display_name, ''), u.username, ''),
               a.created_at
        FROM customer_attachments a
        LEFT JOIN users u ON u.id = a.uploaded_by_user_id
        WHERE a.customer_id = ?
        ORDER BY a.id DESC`, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []CustomerAttachment
	for rows.Next() {
		a, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *a)
	}
	return list, rows.Err()
}

// GetCustomerAttachmentByID 获取单个附件。
func GetCustomerAttachmentByID(id int) (*CustomerAttachment, error) {
	row := DB.QueryRow(`
        SELECT a.id, a.customer_id, a.category, a.file_name, a.stored_name, a.file_size,
               a.mime, a.uploaded_by_user_id, COALESCE(NULLIF(u.display_name, ''), u.username, ''),
               a.created_at
        FROM customer_attachments a
        LEFT JOIN users u ON u.id = a.uploaded_by_user_id
        WHERE a.id = ?`, id)
	return scanAttachment(row)
}

type attachmentScanner interface {
	Scan(dest ...any) error
}

func scanAttachment(s attachmentScanner) (*CustomerAttachment, error) {
	var a CustomerAttachment
	if err := s.Scan(&a.ID, &a.CustomerID, &a.Category, &a.FileName, &a.StoredName,
		&a.FileSize, &a.Mime, &a.UploadedByUserID, &a.UploadedByName, &a.CreatedAt); err != nil {
		return nil, err
	}
	a.URL = "/uploads/customer_files/" + strconv.Itoa(a.CustomerID) + "/" + a.StoredName
	return &a, nil
}

// AddCustomerAttachment 新增附件记录。
func AddCustomerAttachment(a CustomerAttachment) (int64, error) {
	res, err := DB.Exec(`
        INSERT INTO customer_attachments
            (customer_id, category, file_name, stored_name, file_size, mime, uploaded_by_user_id)
        VALUES (?, ?, ?, ?, ?, ?, ?)`,
		a.CustomerID, a.Category, a.FileName, a.StoredName, a.FileSize, a.Mime, a.UploadedByUserID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// DeleteCustomerAttachment 删除附件记录。
func DeleteCustomerAttachment(id int) error {
	_, err := DB.Exec("DELETE FROM customer_attachments WHERE id = ?", id)
	return err
}
