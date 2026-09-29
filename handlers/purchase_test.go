package handlers

import (
	"database/sql"
	"testing"
	"time"
)

func TestPurchaseDraftAllowsIncompleteData(t *testing.T) {
	req := purchaseOrderRequest{
		SaveAsDraft: true,
		Items: []purchaseItemRequest{{
			MaterialName: "待确认物料",
			Quantity:     -1,
			Price:        -2,
		}},
	}

	normalizePurchaseOrderRequest(&req)
	if msg := validatePurchaseOrderRequest(req); msg != "" {
		t.Fatalf("expected incomplete draft to be valid, got %q", msg)
	}
	if req.Status != purchaseStatusDraft {
		t.Fatalf("expected draft status %d, got %d", purchaseStatusDraft, req.Status)
	}
	if req.Items[0].MaterialType != PurchaseMaterialTypeRawMaterial {
		t.Fatalf("expected default material type %q, got %q", PurchaseMaterialTypeRawMaterial, req.Items[0].MaterialType)
	}
	if req.Items[0].Quantity != 0 || req.Items[0].Price != 0 {
		t.Fatalf("expected negative draft values to be reset, got quantity=%v price=%v", req.Items[0].Quantity, req.Items[0].Price)
	}
}

func TestSubmittedPurchaseOrderRequiresItems(t *testing.T) {
	req := purchaseOrderRequest{SaveAsDraft: false}
	normalizePurchaseOrderRequest(&req)

	if msg := validatePurchaseOrderRequest(req); msg != "请至少添加一种采购物料" {
		t.Fatalf("expected missing item error, got %q", msg)
	}
}

func TestPurchaseStockRebuildRequiredSkipsPaymentOnlyUpdate(t *testing.T) {
	purchaseDate := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.Local)
	actualDate := time.Date(2026, time.September, 3, 0, 0, 0, 0, time.Local)
	stored := storedPurchaseItem{
		ID:            11,
		RawMaterialID: 21,
		MaterialName:  "树脂",
		MaterialType:  PurchaseMaterialTypeRawMaterial,
		Spec:          "A级",
		Unit:          "kg",
		Quantity:      10,
		Price:         5,
		StockAdded:    1,
	}
	incoming := purchaseItemRequest{
		ID:            stored.ID,
		RawMaterialID: stored.RawMaterialID,
		MaterialName:  stored.MaterialName,
		MaterialType:  stored.MaterialType,
		Spec:          stored.Spec,
		Unit:          stored.Unit,
		Quantity:      stored.Quantity,
		Price:         stored.Price,
	}
	req := purchaseOrderRequest{
		ID:            7,
		Status:        1,
		PaymentStatus: "已付款",
		Remark:        "付款完成",
	}

	got := purchaseStockRebuildRequired(
		1, 0,
		sql.NullTime{Time: purchaseDate, Valid: true},
		sql.NullTime{Time: actualDate, Valid: true},
		&purchaseDate, &actualDate,
		map[int]storedPurchaseItem{stored.ID: stored},
		map[int]purchaseItemRequest{stored.ID: incoming},
		req,
	)
	if got {
		t.Fatal("payment-only update should not rebuild stock")
	}
}

func TestPurchaseStockRebuildRequiredDetectsStockChanges(t *testing.T) {
	purchaseDate := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.Local)
	actualDate := time.Date(2026, time.September, 3, 0, 0, 0, 0, time.Local)
	stored := storedPurchaseItem{
		ID:            11,
		RawMaterialID: 21,
		MaterialName:  "树脂",
		MaterialType:  PurchaseMaterialTypeRawMaterial,
		Unit:          "kg",
		Quantity:      10,
		Price:         5,
		StockAdded:    1,
	}
	incoming := purchaseItemRequest{
		ID:            stored.ID,
		RawMaterialID: stored.RawMaterialID,
		MaterialName:  stored.MaterialName,
		MaterialType:  stored.MaterialType,
		Unit:          stored.Unit,
		Quantity:      12,
		Price:         stored.Price,
	}
	req := purchaseOrderRequest{ID: 7, Status: 1}

	got := purchaseStockRebuildRequired(
		1, 0,
		sql.NullTime{Time: purchaseDate, Valid: true},
		sql.NullTime{Time: actualDate, Valid: true},
		&purchaseDate, &actualDate,
		map[int]storedPurchaseItem{stored.ID: stored},
		map[int]purchaseItemRequest{stored.ID: incoming},
		req,
	)
	if !got {
		t.Fatal("quantity change should rebuild stock")
	}
}

func TestPurchaseStockRebuildRequiredDetectsArrivalTransition(t *testing.T) {
	req := purchaseOrderRequest{ID: 7, Status: 1}
	got := purchaseStockRebuildRequired(
		0, 0, sql.NullTime{}, sql.NullTime{},
		nil, nil,
		map[int]storedPurchaseItem{}, map[int]purchaseItemRequest{}, req,
	)
	if !got {
		t.Fatal("status transition to arrived should rebuild stock")
	}
}

func TestNormalizePurchaseSummaryScope(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      string
		wantValid bool
	}{
		{name: "default arrived", input: "", want: purchaseSummaryScopeArrived, wantValid: true},
		{name: "arrived", input: "arrived", want: purchaseSummaryScopeArrived, wantValid: true},
		{name: "paid", input: " paid ", want: purchaseSummaryScopePaid, wantValid: true},
		{name: "invalid", input: "unknown", want: "", wantValid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, valid := normalizePurchaseSummaryScope(tt.input)
			if got != tt.want || valid != tt.wantValid {
				t.Fatalf("normalizePurchaseSummaryScope(%q) = (%q, %v), want (%q, %v)", tt.input, got, valid, tt.want, tt.wantValid)
			}
		})
	}
}
func TestPurchaseSummaryConditionUsesScopeSpecificCriteria(t *testing.T) {
	tests := []struct {
		name  string
		scope string
		want  string
	}{
		{name: "arrived", scope: purchaseSummaryScopeArrived, want: "po.status = 1"},
		{name: "paid", scope: purchaseSummaryScopePaid, want: "po.status IN (0, 1) AND po.payment_status = '已付款'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := purchaseSummaryCondition(tt.scope); got != tt.want {
				t.Fatalf("purchaseSummaryCondition(%q) = %q, want %q", tt.scope, got, tt.want)
			}
		})
	}
}
