# order_system

订单、库存、采购与成本管理系统。

## 库存核算

系统按四本账分离记录：

- 实际库存：`products.stock` / `raw_materials.stock`，只由真实出入库、生产领料、盘点和冲回动作改变，不允许负数。
- 订单占用：`inventory_reservations`，下单只占用可用成品库存，取消只释放占用。
- 采购需求：由实际库存、订单占用、生产短缺、在途采购和安全库存实时计算，净需求可显示为负数。
- 成本流水：`stock_movements` 记录每次入库、出库、领料、完工和冲回的数量、单位成本及金额；成品移动平均成本保存在 `products.avg_cost`，原材料移动平均成本保存在 `raw_materials.avg_cost`。

启动时 `models.EnsureInventoryAccounting` 会自动创建新账表。历史负库存会归零，并写为 `legacy_shortage` 记录，后续继续计入净需求，不再进入账面库存。

## 主要接口

- `GET /api/purchase-materials/demand`：原材料净需求与建议采购量。
- `GET /api/stock-movements?item_type=raw_material&item_id=1`：库存与成本流水。