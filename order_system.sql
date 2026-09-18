-- =============================================
-- 订单库存管理系统 - 数据库脚本
-- 适用环境：MySQL 5.7+
-- 字符集：utf8mb4
-- 设计说明：
--   - 产品（成品）库存：INT 整数（如 100 瓶）
--   - 原材料库存：DECIMAL(10,3) 支持小数（如 0.500 升）
--   - BOM 用量：DECIMAL(10,3) 支持小数（如 0.1 升）
-- =============================================

-- 1. 创建数据库（如果不存在）
CREATE DATABASE IF NOT EXISTS order_system 
    DEFAULT CHARSET utf8mb4 
    COLLATE utf8mb4_unicode_ci;

USE order_system;

-- 2. 删除已存在的表（按依赖顺序倒序删除）
DROP TABLE IF EXISTS stock_movements;
DROP TABLE IF EXISTS inventory_reservations;
DROP TABLE IF EXISTS order_bom_snapshot;
DROP TABLE IF EXISTS product_bom;
DROP TABLE IF EXISTS order_items;
DROP TABLE IF EXISTS orders;
DROP TABLE IF EXISTS raw_materials;
DROP TABLE IF EXISTS products;
DROP TABLE IF EXISTS purchase_materials;
DROP TABLE IF EXISTS purchase_orders;
DROP TABLE IF EXISTS expenses;

-- =============================================
-- 3. 创建产品表（成品库存）
--    库存为整数（如：100瓶、50箱）
-- =============================================
CREATE TABLE products (
    id          INT AUTO_INCREMENT PRIMARY KEY COMMENT '产品ID',
    name        VARCHAR(100) NOT NULL COMMENT '产品名称',
    spec        VARCHAR(100) NOT NULL DEFAULT '' COMMENT '规格型号',
    unit        VARCHAR(20)  NOT NULL DEFAULT '个' COMMENT '单位',
    stock       INT NOT NULL DEFAULT 0 COMMENT '实际库存数量（整数，不允许负数）',
    avg_cost    DECIMAL(14,4) NOT NULL DEFAULT 0 COMMENT '移动加权平均成本',
    price       DECIMAL(10,2) NOT NULL COMMENT '销售单价',
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='成品库存表';

-- =============================================
-- 4. 创建原材料表
--    库存为 DECIMAL(10,3) 支持小数（如：0.5升）
-- =============================================
CREATE TABLE raw_materials (
    id          INT AUTO_INCREMENT PRIMARY KEY COMMENT '原材料ID',
    name        VARCHAR(100) NOT NULL COMMENT '原材料名称',
    spec        VARCHAR(100) NOT NULL DEFAULT '' COMMENT '规格型号',
    stock       DECIMAL(10,3) NOT NULL DEFAULT 0 COMMENT '实际库存数量（支持小数，不允许负数）',
    avg_cost    DECIMAL(14,4) NOT NULL DEFAULT 0 COMMENT '移动加权平均成本',
    unit        VARCHAR(20) DEFAULT '个' COMMENT '单位（如：升、千克、米）',
    min_stock   DECIMAL(10,3) DEFAULT 0 COMMENT '最低库存预警值（支持小数）',
    price       DECIMAL(10,2) DEFAULT 0 COMMENT '原材料单价',
    images      TEXT COMMENT '原材料图鉴图片路径(JSON数组)',
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='原材料库存表';

-- =============================================
-- 4.5 创建采购单主表
-- =============================================
CREATE TABLE purchase_orders (
    id                    INT AUTO_INCREMENT PRIMARY KEY COMMENT '采购单ID',
    purchase_no           VARCHAR(32) NOT NULL UNIQUE COMMENT '采购单号',
    supplier              VARCHAR(100) NOT NULL DEFAULT '' COMMENT '供应商',
    freight               DECIMAL(10,2) NOT NULL DEFAULT 0 COMMENT '运费',
    purchase_date         DATE NULL COMMENT '采购日期',
    expected_arrival_date DATE NULL COMMENT '预计到货日期',
    actual_arrival_date   DATE NULL COMMENT '实际到货日期',
    payment_status        VARCHAR(20) NOT NULL DEFAULT '未付款' COMMENT '付款状态',
    status                TINYINT NOT NULL DEFAULT 0 COMMENT '0-采购中 1-已到货',
    remark                TEXT COMMENT '备注',
    payment_receipt       TEXT COMMENT '支付水单图片路径(JSON数组)',
    created_at            DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='采购单主表';

-- =============================================
-- 4.6 创建采购物料明细表
-- =============================================
CREATE TABLE purchase_materials (
    id                    INT AUTO_INCREMENT PRIMARY KEY COMMENT '采购物料明细ID',
    purchase_order_id     INT NULL COMMENT '采购单ID',
    raw_material_id       INT NOT NULL DEFAULT 0 COMMENT '关联原材料ID',
    material_name         VARCHAR(100) NOT NULL COMMENT '物料名称',
    material_type         VARCHAR(20)  NOT NULL DEFAULT '原材料' COMMENT '物料类型',
    spec                  VARCHAR(100) NOT NULL DEFAULT '' COMMENT '规格型号',
    unit                  VARCHAR(20)  NOT NULL DEFAULT '个' COMMENT '单位',
    quantity              DECIMAL(12,3) NOT NULL DEFAULT 0 COMMENT '采购数量',
    price                 DECIMAL(10,2) NOT NULL DEFAULT 0 COMMENT '单价',
    amount                DECIMAL(12,2) NOT NULL DEFAULT 0 COMMENT '金额',
    supplier              VARCHAR(100) NOT NULL DEFAULT '' COMMENT '供应商',
    freight               DECIMAL(10,2) NOT NULL DEFAULT 0 COMMENT '运费',
    purchase_date         DATE NULL COMMENT '采购日期',
    expected_arrival_date DATE NULL COMMENT '预计到货日期',
    actual_arrival_date   DATE NULL COMMENT '实际到货日期',
    status                TINYINT NOT NULL DEFAULT 0 COMMENT '0-采购中 1-已到货',
    payment_status        VARCHAR(20) NOT NULL DEFAULT '未付款' COMMENT '付款状态',
    remark                TEXT COMMENT '备注',
    payment_receipt       TEXT COMMENT '支付水单图片路径(JSON数组)',
    stock_added           TINYINT NOT NULL DEFAULT 0 COMMENT '是否已加入原材料库存',
    created_at            DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    INDEX idx_purchase_material_order_id (purchase_order_id),
    INDEX idx_purchase_material_raw_material_id (raw_material_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='采购物料明细表';

-- =============================================
-- 4.7 创建费用记录表
-- =============================================
CREATE TABLE expenses (
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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='费用记录表';
-- =============================================
-- 5. 创建订单主表
-- =============================================
CREATE TABLE orders (
    id              INT AUTO_INCREMENT PRIMARY KEY COMMENT '订单ID',
    order_no        VARCHAR(32) NOT NULL UNIQUE COMMENT '订单编号',
    customer_name   VARCHAR(100) NOT NULL COMMENT '客户名称',
    region          VARCHAR(100) NOT NULL DEFAULT '' COMMENT '地区',
    customer_address VARCHAR(255) DEFAULT '' COMMENT '客户地址',
    customer_phone  VARCHAR(20) DEFAULT '' COMMENT '客户电话',
    delivery_date   DATE DEFAULT NULL COMMENT '期望交货日期',
    order_date      DATE DEFAULT NULL COMMENT '下单日期',
    expected_shipping_date DATE DEFAULT NULL COMMENT '预计发货日期',
    customer_required_date DATE DEFAULT NULL COMMENT '客户需求到货日',
    logistics_days INT NOT NULL DEFAULT 0 COMMENT '物流天数（天）',
    total_amount    DECIMAL(10,2) NOT NULL COMMENT '订单总金额',
    status          TINYINT DEFAULT 0 COMMENT '0-待生产 1-生产中 2-待发货 3-已发货 4-已取消',
    payment_status  TINYINT DEFAULT 0 COMMENT '回款状态：0-否 1-是',
    prepared_by     VARCHAR(100) NOT NULL DEFAULT '' COMMENT '制单人',
    payment_settlement VARCHAR(50) NOT NULL DEFAULT '现付' COMMENT '货款结算方式：现付/自定义',
    freight_payment VARCHAR(50) NOT NULL DEFAULT '现付' COMMENT '运费支付方式：现付/自定义',
    freight_recovery VARCHAR(20) NOT NULL DEFAULT '可回收' COMMENT '运费回收：可回收/不可回收',
    transport_method VARCHAR(50) NOT NULL DEFAULT '物流' COMMENT '运输方式：物流/快递/自定义',
    remark          TEXT COMMENT '备注',
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='订单主表';

-- =============================================
-- 6. 创建订单明细表
-- =============================================
CREATE TABLE order_items (
    id          INT AUTO_INCREMENT PRIMARY KEY COMMENT '明细ID',
    order_id    INT NOT NULL COMMENT '订单ID',
    product_id  INT NOT NULL COMMENT '产品ID',
    quantity    INT NOT NULL COMMENT '购买数量（整数）',
    price       DECIMAL(10,2) NOT NULL COMMENT '下单时单价（快照）',
    FOREIGN KEY (order_id)   REFERENCES orders(id) ON DELETE CASCADE,
    FOREIGN KEY (product_id) REFERENCES products(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='订单明细表';

-- =============================================
-- 7. 创建产品BOM表（产品-原材料关联）
--    用量为 DECIMAL(10,3) 支持小数（如：0.1升）
-- =============================================
CREATE TABLE product_bom (
    id              INT AUTO_INCREMENT PRIMARY KEY COMMENT 'BOM记录ID',
    product_id      INT NOT NULL COMMENT '产品ID',
    raw_material_id INT NOT NULL COMMENT '原材料ID',
    quantity        DECIMAL(10,3) NOT NULL COMMENT '生产1个单位产品所需原材料数量（支持小数）',
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    FOREIGN KEY (product_id)      REFERENCES products(id) ON DELETE CASCADE,
    FOREIGN KEY (raw_material_id) REFERENCES raw_materials(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='产品BOM表';

-- =============================================
-- 7.2 创建订单BOM快照表
--    下单时按当前BOM写入快照，供历史订单追溯；当前生产采购需求按实际BOM计算，
--    避免之后修改产品BOM影响历史订单。
-- =============================================
CREATE TABLE order_bom_snapshot (
    id              INT AUTO_INCREMENT PRIMARY KEY COMMENT '快照ID',
    order_id        INT NOT NULL COMMENT '订单ID',
    product_id      INT NOT NULL COMMENT '产品ID',
    raw_material_id INT NOT NULL COMMENT '原材料ID',
    quantity        DECIMAL(10,3) NOT NULL COMMENT '该订单需耗用的原材料数量（按下单时BOM计算）',
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    KEY idx_order_bom_snapshot_order_id (order_id),
    FOREIGN KEY (order_id)        REFERENCES orders(id) ON DELETE CASCADE,
    FOREIGN KEY (product_id)      REFERENCES products(id) ON DELETE CASCADE,
    FOREIGN KEY (raw_material_id) REFERENCES raw_materials(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='订单BOM快照表';

-- =============================================
-- 7.3 库存与成本流水（不可变账）
-- =============================================
CREATE TABLE stock_movements (
    id                  BIGINT AUTO_INCREMENT PRIMARY KEY,
    item_type           VARCHAR(20) NOT NULL COMMENT 'product/raw_material',
    item_id             INT NOT NULL,
    movement_type       VARCHAR(40) NOT NULL,
    quantity            DECIMAL(14,3) NOT NULL COMMENT '带方向，入库为正出库为负',
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
    KEY idx_stock_movement_reference (reference_type, reference_id),
    KEY idx_stock_movement_order_item (order_item_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='不可变库存与成本流水';

-- =============================================
-- 7.4 订单占用账
-- =============================================
CREATE TABLE inventory_reservations (
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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='订单占用账';
-- 7.5 创建系统用户表（登录鉴权）
--     角色：admin-管理员（全部权限），user-普通用户（仅查看）
-- =============================================
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS customers;
CREATE TABLE users (
    id            INT AUTO_INCREMENT PRIMARY KEY COMMENT '用户ID',
    username      VARCHAR(50)  NOT NULL UNIQUE COMMENT '登录用户名',
    password_hash VARCHAR(255) NOT NULL COMMENT '密码哈希（PBKDF2-SHA256）',
    display_name  VARCHAR(100) NOT NULL DEFAULT '' COMMENT '显示名称',
    role          VARCHAR(20)  NOT NULL DEFAULT 'user' COMMENT '角色: admin-管理员, user-普通用户(只读)',
    created_at    DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='系统用户表';

-- 默认账户（登录后请尽快修改密码）
-- 管理员：admin / admin123
-- 普通用户：viewer / viewer123
INSERT INTO users (username, password_hash, display_name, role) VALUES
('admin',  'pbkdf2$210000$65c49fa4fc44338243ff5670c9cbed34$3939aaf52c164157641e0d73027a69a530f42c439ec4f3828f2408c1be2612ec', '管理员', 'admin'),
('viewer', 'pbkdf2$210000$663b5044bee25b02cd34c9d299c45dc2$3554c2956f826e93200f4fd1cc82dd84d5364b926c5c6773f7cdc5ed6d83b32d', '普通用户', 'user');

-- =============================================
-- 7.8 创建客户表
-- =============================================
CREATE TABLE customers (
    id          INT AUTO_INCREMENT PRIMARY KEY COMMENT '客户ID',
    name        VARCHAR(100) NOT NULL COMMENT '客户名称',
    phone       VARCHAR(20)  NOT NULL DEFAULT '' COMMENT '电话号码',
    address     VARCHAR(255) NOT NULL DEFAULT '' COMMENT '收货地址',
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='客户表';
-- 8. 创建索引（优化查询性能）
-- =============================================
CREATE INDEX idx_orders_status ON orders(status);
CREATE INDEX idx_orders_created_at ON orders(created_at);
CREATE INDEX idx_order_items_order_id ON order_items(order_id);
CREATE INDEX idx_order_items_product_id ON order_items(product_id);
CREATE INDEX idx_product_bom_product_id ON product_bom(product_id);
CREATE INDEX idx_product_bom_raw_material_id ON product_bom(raw_material_id);
