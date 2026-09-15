package main

import (
	"log"
	"net/http"
	"order-system/handlers"
	"order-system/models"
	"os"
	"path/filepath"
)

// page 返回一个渲染指定模板文件（含 layout）的页面处理器，
// 数据中会自动注入当前登录用户与权限信息。
func page(files ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handlers.RenderPage(w, r, files...)
	}
}

func main() {
	// 若可执行文件所在目录包含 templates（部署目录），则切换为该目录作为工作目录，
	// 避免 systemd/脚本从其它目录启动时找不到 templates/、static/、uploads/。
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		if _, err := os.Stat(filepath.Join(exeDir, "templates")); err == nil {
			if err := os.Chdir(exeDir); err == nil {
				log.Printf("工作目录已切换到程序所在目录: %s", exeDir)
			}
		}
	}

	// 初始化数据库（同时确保 users 表存在并写入默认账户）
	models.InitDB("root:Wxl111222@tcp(127.0.0.1:3306)/order_system?charset=utf8mb4&parseTime=True")

	// 静态文件
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))))
	http.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir("./uploads"))))

	// 登录相关（无需登录）
	http.HandleFunc("/login", handlers.LoginPage)
	http.HandleFunc("/api/login", handlers.Login)
	http.HandleFunc("/api/logout", handlers.Logout)

	// 页面路由（需登录）
	http.HandleFunc("/", handlers.RequireAuth(page("templates/layout.html", "templates/index.html")))
	http.HandleFunc("/orders", handlers.RequireAnyPermission(handlers.PermOrderViewOwn, handlers.PermOrderViewAll)(page("templates/layout.html", "templates/orders.html")))
	http.HandleFunc("/products", handlers.RequirePermission(handlers.PermProductView, page("templates/layout.html", "templates/products.html")))
	http.HandleFunc("/raw-materials", handlers.RequirePermission(handlers.PermMaterialView, page("templates/layout.html", "templates/raw_materials.html")))
	http.HandleFunc("/purchase-materials", handlers.RequirePermission(handlers.PermPurchaseView, page("templates/layout.html", "templates/purchase_materials.html")))
	http.HandleFunc("/customers", handlers.RequirePermission(handlers.PermCustomerView, page("templates/layout.html", "templates/customers.html")))
	http.HandleFunc("/customers/detail", handlers.RequirePermission(handlers.PermCustomerView, page("templates/layout.html", "templates/customer_detail.html")))
	http.HandleFunc("/order/detail", handlers.RequireAnyPermission(handlers.PermOrderViewOwn, handlers.PermOrderViewAll)(handlers.OrderDetailPage))

	// 用户管理页面（仅管理员）
	http.HandleFunc("/users", handlers.RequirePermission(handlers.PermUserManage, page("templates/layout.html", "templates/users.html")))

	// 当前用户信息 / 修改密码（需登录）
	http.HandleFunc("/api/me", handlers.RequireAuth(handlers.CurrentUserInfo))
	http.HandleFunc("/api/change-password", handlers.RequireAuth(handlers.ChangePassword))
	http.HandleFunc("/api/change-name", handlers.RequireAuth(handlers.ChangeDisplayName))

	// API 路由 - 产品（列表/详情需登录，写操作需 product:manage 权限）
	http.HandleFunc("/api/products", handlers.RequireAnyPermission(handlers.PermProductView, handlers.PermOrderViewAll, handlers.PermOrderViewOwn)(handlers.ListProducts))
	http.HandleFunc("/api/products/get", handlers.RequireAnyPermission(handlers.PermProductView, handlers.PermOrderViewAll, handlers.PermOrderViewOwn)(handlers.GetProduct))
	http.HandleFunc("/api/products/add", handlers.RequirePermission(handlers.PermProductManage, handlers.AddProduct))
	http.HandleFunc("/api/products/update", handlers.RequirePermission(handlers.PermProductManage, handlers.UpdateProduct))
	http.HandleFunc("/api/products/delete", handlers.RequirePermission(handlers.PermProductManage, handlers.DeleteProduct))
	http.HandleFunc("/api/production", handlers.RequirePermission(handlers.PermProductManage, handlers.ProduceProduct))
	http.HandleFunc("/outbound/detail", handlers.RequirePermission(handlers.PermProductView, handlers.ProductOutboundPrintPage))
	http.HandleFunc("/api/outbounds/list", handlers.RequirePermission(handlers.PermProductView, handlers.ListProductOutbounds))
	http.HandleFunc("/api/outbounds/create", handlers.RequirePermission(handlers.PermProductManage, handlers.CreateProductOutbound))
	http.HandleFunc("/api/outbounds/delete", handlers.RequirePermission(handlers.PermProductManage, handlers.DeleteProductOutbound))

	// API 路由 - 订单
	http.HandleFunc("/api/orders", handlers.RequireAnyPermission(handlers.PermOrderEditOwn, handlers.PermOrderEditAll)(handlers.CreateOrder))
	http.HandleFunc("/api/orders/list", handlers.RequireAnyPermission(handlers.PermOrderViewOwn, handlers.PermOrderViewAll)(handlers.GetOrders))
	http.HandleFunc("/api/customers/orders", handlers.RequireAnyPermission(handlers.PermOrderViewOwn, handlers.PermOrderViewAll)(handlers.GetCustomerOrders))
	http.HandleFunc("/api/orders/detail", handlers.RequireAnyPermission(handlers.PermOrderViewOwn, handlers.PermOrderViewAll)(handlers.GetOrderDetail))
	http.HandleFunc("/api/orders/summary", handlers.RequireAnyPermission(handlers.PermOrderViewOwn, handlers.PermOrderViewAll)(handlers.GetOrderSummary))
	http.HandleFunc("/api/orders/status", handlers.RequireAnyPermission(handlers.PermOrderEditOwn, handlers.PermOrderEditAll)(handlers.UpdateOrderStatus))
	http.HandleFunc("/api/orders/delete", handlers.RequirePermission(handlers.PermOrderEditAll, handlers.DeleteOrder))
	http.HandleFunc("/api/orders/update", handlers.RequireAnyPermission(handlers.PermOrderEditOwn, handlers.PermOrderEditAll)(handlers.UpdateOrder))

	// API 路由 - 客户
	http.HandleFunc("/api/customers", handlers.RequireAnyPermission(handlers.PermCustomerView, handlers.PermOrderViewAll, handlers.PermOrderViewOwn)(handlers.ListCustomers))
	http.HandleFunc("/api/customers/get", handlers.RequireAnyPermission(handlers.PermCustomerView, handlers.PermOrderViewAll, handlers.PermOrderViewOwn)(handlers.GetCustomer))
	http.HandleFunc("/api/customers/add", handlers.RequirePermission(handlers.PermCustomerManage, handlers.AddCustomer))
	http.HandleFunc("/api/customers/update", handlers.RequirePermission(handlers.PermCustomerManage, handlers.UpdateCustomer))
	http.HandleFunc("/api/customers/delete", handlers.RequirePermission(handlers.PermCustomerManage, handlers.DeleteCustomer))
	http.HandleFunc("/api/customers/contacts", handlers.RequirePermission(handlers.PermCustomerView, handlers.ListCustomerContacts))
	http.HandleFunc("/api/customers/contacts/add", handlers.RequirePermission(handlers.PermCustomerManage, handlers.AddCustomerContact))
	http.HandleFunc("/api/customers/contacts/update", handlers.RequirePermission(handlers.PermCustomerManage, handlers.UpdateCustomerContact))
	http.HandleFunc("/api/customers/contacts/delete", handlers.RequirePermission(handlers.PermCustomerManage, handlers.DeleteCustomerContact))
	http.HandleFunc("/api/customers/attachments", handlers.RequirePermission(handlers.PermCustomerView, handlers.ListCustomerAttachments))
	http.HandleFunc("/api/customers/attachments/upload", handlers.RequirePermission(handlers.PermCustomerManage, handlers.UploadCustomerAttachment))
	http.HandleFunc("/api/customers/attachments/delete", handlers.RequirePermission(handlers.PermCustomerManage, handlers.DeleteCustomerAttachment))

	// API 路由 - 采购物料
	http.HandleFunc("/api/purchase-materials", handlers.RequirePermission(handlers.PermPurchaseView, handlers.ListPurchaseMaterials))
	http.HandleFunc("/api/purchase-materials/raw-material-options", handlers.RequirePermission(handlers.PermPurchaseView, handlers.ListPurchaseRawMaterialOptions))
	http.HandleFunc("/api/purchase-materials/summary", handlers.RequirePermission(handlers.PermPurchaseView, handlers.GetPurchaseMaterialsSummary))
	http.HandleFunc("/api/purchase-materials/get", handlers.RequirePermission(handlers.PermPurchaseView, handlers.GetPurchaseMaterial))
	http.HandleFunc("/api/purchase-materials/add", handlers.RequirePermission(handlers.PermPurchaseManage, handlers.AddPurchaseMaterial))
	http.HandleFunc("/api/purchase-materials/update", handlers.RequirePermission(handlers.PermPurchaseManage, handlers.UpdatePurchaseMaterial))
	http.HandleFunc("/api/purchase-materials/delete", handlers.RequirePermission(handlers.PermPurchaseManage, handlers.DeletePurchaseMaterial))
	http.HandleFunc("/api/purchase-materials/upload", handlers.RequirePermission(handlers.PermPurchaseManage, handlers.UploadPurchaseReceipt))

	// API 路由 - 原材料（列表/详情/预警需登录，写操作需 material:manage 权限）
	http.HandleFunc("/api/raw-materials", handlers.RequireAnyPermission(handlers.PermMaterialView, handlers.PermProductView)(handlers.ListRawMaterials))
	http.HandleFunc("/api/raw-materials/get", handlers.RequireAnyPermission(handlers.PermMaterialView, handlers.PermProductView)(handlers.GetRawMaterial))
	http.HandleFunc("/api/raw-materials/add", handlers.RequirePermission(handlers.PermMaterialManage, handlers.AddRawMaterial))
	http.HandleFunc("/api/raw-materials/update", handlers.RequirePermission(handlers.PermMaterialManage, handlers.UpdateRawMaterial))
	http.HandleFunc("/api/raw-materials/upload", handlers.RequirePermission(handlers.PermMaterialManage, handlers.UploadRawMaterialImages))
	http.HandleFunc("/api/raw-materials/delete", handlers.RequirePermission(handlers.PermMaterialManage, handlers.DeleteRawMaterial))
	http.HandleFunc("/api/raw-materials/inbound", handlers.RequirePermission(handlers.PermMaterialManage, handlers.RawMaterialInbound))
	http.HandleFunc("/api/raw-materials/outbound", handlers.RequirePermission(handlers.PermMaterialManage, handlers.RawMaterialOutbound))
	http.HandleFunc("/api/raw-materials/low-stock", handlers.RequireAnyPermission(handlers.PermMaterialView, handlers.PermProductView)(handlers.GetLowStockRawMaterials))

	// API 路由 - 用户管理（仅管理员）
	http.HandleFunc("/api/users", handlers.RequirePermission(handlers.PermUserManage, handlers.ListUsers))
	http.HandleFunc("/api/users/options", handlers.RequireAuth(handlers.UserOptions))
	http.HandleFunc("/api/users/add", handlers.RequirePermission(handlers.PermUserManage, handlers.AddUser))
	http.HandleFunc("/api/users/delete", handlers.RequirePermission(handlers.PermUserManage, handlers.DeleteUser))
	http.HandleFunc("/api/users/reset-password", handlers.RequirePermission(handlers.PermUserManage, handlers.ResetPassword))
	http.HandleFunc("/api/users/permissions", handlers.RequirePermission(handlers.PermUserManage, handlers.SaveUserPermissions))

	os.MkdirAll("uploads/payment_receipts", 0755)
	os.MkdirAll("uploads/raw_material_images", 0755)
	log.Println("Server started at :6688")
	log.Fatal(http.ListenAndServe(":6688", nil))
}
