package handlers

import (
	"bytes"
	"encoding/json"
	"html/template"
	"io"
	"order-system/models"
	"order-system/utils"
	"strings"
	"testing"
	"time"
)

func makePageData(user *models.User) PageData {
	var userJSON template.JS = "null"
	if user != nil {
		payload := map[string]interface{}{
			"id": user.ID, "username": user.Username,
			"display_name": user.DisplayName, "role": user.Role,
			"is_admin": user.IsAdmin(),
		}
		if b, err := json.Marshal(payload); err == nil {
			userJSON = template.JS(b)
		}
	}
	perms := map[string]bool{}
	if user != nil {
		if user.IsAdmin() {
			for _, p := range allPermissions {
				perms[p] = true
			}
		} else {
			// 测试模拟已迁移的普通用户：数据分析、费用管理默认不授权。
			for _, g := range grantableGroups {
				if g[0] == PermAnalyticsView || g[0] == PermExpenseView {
					continue
				}
				perms[g[0]] = true
			}
		}
	}
	permBytes, _ := json.Marshal(perms)
	return PageData{
		User:      user,
		UserJSON:  userJSON,
		Perms:     perms,
		PermsJSON: template.JS(permBytes),
	}
}

func TestPageTemplates(t *testing.T) {
	admin := &models.User{ID: 1, Username: "admin", DisplayName: "管理员", Role: models.RoleAdmin, CreatedAt: time.Now()}
	viewer := &models.User{ID: 2, Username: "viewer", DisplayName: "普通用户", Role: models.RoleUser, CreatedAt: time.Now()}

	cases := []struct {
		name  string
		files []string
		user  *models.User
	}{
		{"index-admin", []string{"../templates/layout.html", "../templates/index.html"}, admin},
		{"index-viewer", []string{"../templates/layout.html", "../templates/index.html"}, viewer},
		{"orders-admin", []string{"../templates/layout.html", "../templates/orders.html"}, admin},
		{"orders-viewer", []string{"../templates/layout.html", "../templates/orders.html"}, viewer},
		{"inventory-admin", []string{"../templates/layout.html", "../templates/inventory.html"}, admin},
		{"inventory-viewer", []string{"../templates/layout.html", "../templates/inventory.html"}, viewer},
		{"products-admin", []string{"../templates/layout.html", "../templates/products.html"}, admin},
		{"products-viewer", []string{"../templates/layout.html", "../templates/products.html"}, viewer},
		{"rawmaterials-admin", []string{"../templates/layout.html", "../templates/raw_materials.html"}, admin},
		{"rawmaterials-viewer", []string{"../templates/layout.html", "../templates/raw_materials.html"}, viewer},
		{"analytics-admin", []string{"../templates/layout.html", "../templates/analytics.html"}, admin},
		{"analytics-viewer", []string{"../templates/layout.html", "../templates/analytics.html"}, viewer},
		{"purchase-admin", []string{"../templates/layout.html", "../templates/purchase_materials.html"}, admin},
		{"purchase-viewer", []string{"../templates/layout.html", "../templates/purchase_materials.html"}, viewer},
		{"expenses-admin", []string{"../templates/layout.html", "../templates/expenses.html"}, admin},
		{"expenses-viewer", []string{"../templates/layout.html", "../templates/expenses.html"}, viewer},
		{"expense-form-admin", []string{"../templates/layout.html", "../templates/expense_form.html"}, admin},
		{"expense-form-viewer", []string{"../templates/layout.html", "../templates/expense_form.html"}, viewer},
		{"customers-admin", []string{"../templates/layout.html", "../templates/customers.html"}, admin},
		{"customers-viewer", []string{"../templates/layout.html", "../templates/customers.html"}, viewer},
		{"customer-detail-admin", []string{"../templates/layout.html", "../templates/customer_detail.html"}, admin},
		{"customer-detail-viewer", []string{"../templates/layout.html", "../templates/customer_detail.html"}, viewer},
		{"order-form-admin", []string{"../templates/layout.html", "../templates/order_form.html"}, admin},
		{"order-form-viewer", []string{"../templates/layout.html", "../templates/order_form.html"}, viewer},
		{"product-form-admin", []string{"../templates/layout.html", "../templates/product_form.html"}, admin},
		{"product-form-viewer", []string{"../templates/layout.html", "../templates/product_form.html"}, viewer},
		{"purchase-form-admin", []string{"../templates/layout.html", "../templates/purchase_form.html"}, admin},
		{"purchase-form-viewer", []string{"../templates/layout.html", "../templates/purchase_form.html"}, viewer},
		{"customer-form-admin", []string{"../templates/layout.html", "../templates/customer_form.html"}, admin},
		{"customer-form-viewer", []string{"../templates/layout.html", "../templates/customer_form.html"}, viewer},
		{"users-admin", []string{"../templates/layout.html", "../templates/users.html"}, admin},
	}

	for _, c := range cases {
		tmpl, err := template.ParseFiles(c.files...)
		if err != nil {
			t.Fatalf("[%s] parse error: %v", c.name, err)
		}
		if err := tmpl.Execute(io.Discard, makePageData(c.user)); err != nil {
			t.Fatalf("[%s] execute error: %v", c.name, err)
		}
	}

	// 登录页（无用户数据）
	if tmpl, err := template.ParseFiles("../templates/login.html"); err != nil {
		t.Fatalf("login parse error: %v", err)
	} else if err := tmpl.Execute(io.Discard, nil); err != nil {
		t.Fatalf("login execute error: %v", err)
	}

	// 订单详情页（独立模板，使用 utils.FuncMap）
	detailData := struct {
		Order models.Order
		Items []struct {
			ProductID   int
			ProductName string
			Spec        string
			Unit        string
			Quantity    int
			Price       float64
		}
		Outbounds     []models.ProductOutbound
		Now           time.Time
		TotalQuantity int
	}{
		Order: models.Order{OrderNo: "ORDTEST", CustomerName: "测试客户", Status: 0, CreatedAt: time.Now(), TotalAmount: 100},
		Items: []struct {
			ProductID   int
			ProductName string
			Spec        string
			Unit        string
			Quantity    int
			Price       float64
		}{{ProductID: 1, ProductName: "产品A", Spec: "A型", Unit: "个", Quantity: 2, Price: 50}},
		Outbounds:     []models.ProductOutbound{},
		Now:           time.Now(),
		TotalQuantity: 2,
	}
	tmpl, err := template.New("order_detail.html").Funcs(utils.FuncMap()).ParseFiles("../templates/order_detail.html")
	if err != nil {
		t.Fatalf("order_detail parse error: %v", err)
	}
	if err := tmpl.Execute(io.Discard, detailData); err != nil {
		t.Fatalf("order_detail execute error: %v", err)
	}

	// 成品出库送货单打印页（独立模板）
	outboundData := struct {
		Outbound      models.ProductOutbound
		Items         []models.ProductOutboundItem
		Now           time.Time
		TotalQuantity float64
	}{
		Outbound: models.ProductOutbound{OutboundNo: "FHTEST", OutDate: time.Now(), Receiver: "测试客户", CreatedByName: "管理员"},
		Items: []models.ProductOutboundItem{
			{ProductName: "产品A", Spec: "A型", Unit: "个", Quantity: 2},
		},
		Now:           time.Now(),
		TotalQuantity: 2,
	}
	tmpl2, err := template.New("product_outbound.html").Funcs(utils.FuncMap()).ParseFiles("../templates/product_outbound.html")
	if err != nil {
		t.Fatalf("product_outbound parse error: %v", err)
	}
	if err := tmpl2.Execute(io.Discard, outboundData); err != nil {
		t.Fatalf("product_outbound execute error: %v", err)
	}
}

func TestAnalyticsVisibilityAndHomeOrder(t *testing.T) {
	admin := &models.User{ID: 1, Username: "admin", DisplayName: "管理员", Role: models.RoleAdmin, CreatedAt: time.Now()}
	viewer := &models.User{ID: 2, Username: "viewer", DisplayName: "普通用户", Role: models.RoleUser, CreatedAt: time.Now()}

	tmpl, err := template.ParseFiles("../templates/layout.html", "../templates/index.html")
	if err != nil {
		t.Fatalf("parse index template: %v", err)
	}
	render := func(user *models.User) string {
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, makePageData(user)); err != nil {
			t.Fatalf("render index template: %v", err)
		}
		return buf.String()
	}

	adminHTML := render(admin)
	viewerHTML := render(viewer)
	if !strings.Contains(adminHTML, "数据分析") {
		t.Fatal("管理员首页应显示数据分析")
	}
	if strings.Contains(viewerHTML, "数据分析") {
		t.Fatal("普通用户默认不应看到数据分析")
	}
	if !strings.Contains(adminHTML, "费用管理") {
		t.Fatal("管理员首页应显示费用管理")
	}
	if strings.Contains(viewerHTML, "费用管理") {
		t.Fatal("普通用户默认不应看到费用管理")
	}
	if strings.Index(adminHTML, "采购物料管理") > strings.Index(adminHTML, "费用管理") ||
		strings.Index(adminHTML, "费用管理") > strings.Index(adminHTML, "客户管理") {
		t.Fatal("费用管理应位于采购物料管理之后、客户管理之前")
	}
	if strings.LastIndex(adminHTML, "数据分析") < strings.Index(adminHTML, "客户管理") {
		t.Fatal("数据分析应位于首页模块最后")
	}
}

func TestExpensePermissionGrant(t *testing.T) {
	viewPerms, err := NormalizeGrantedPermissions([]string{PermExpenseView})
	if err != nil {
		t.Fatalf("normalize expense view permission: %v", err)
	}
	if len(viewPerms) != 1 || viewPerms[0] != PermExpenseView {
		t.Fatalf("unexpected expense view permissions: %#v", viewPerms)
	}

	managePerms, err := NormalizeGrantedPermissions([]string{PermExpenseManage})
	if err != nil {
		t.Fatalf("normalize expense manage permission: %v", err)
	}
	granted := map[string]bool{}
	for _, permission := range managePerms {
		granted[permission] = true
	}
	if !granted[PermExpenseView] || !granted[PermExpenseManage] {
		t.Fatalf("expense manage should imply view, got: %#v", managePerms)
	}
}
