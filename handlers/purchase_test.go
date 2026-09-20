package handlers

import "testing"

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
