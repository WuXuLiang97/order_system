package handlers

import "testing"

func TestNormalizeProductionItemsMergesDuplicates(t *testing.T) {
	merged, err := normalizeProductionItems([]productionItemRequest{
		{ProductID: 11, Quantity: 2},
		{ProductID: 22, Quantity: 1},
		{ProductID: 11, Quantity: 3.5},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(merged) != 2 {
		t.Fatalf("expected 2 merged rows, got %d", len(merged))
	}
	if merged[0].ProductID != 11 || merged[0].Quantity != 5.5 {
		t.Fatalf("expected product 11 quantity 5.5, got %#v", merged[0])
	}
	if merged[1].ProductID != 22 || merged[1].Quantity != 1 {
		t.Fatalf("expected product 22 quantity 1, got %#v", merged[1])
	}
}

func TestNormalizeProductionItemsRejectsInvalidRows(t *testing.T) {
	cases := []struct {
		name  string
		items []productionItemRequest
	}{
		{"empty", nil},
		{"missing product", []productionItemRequest{{ProductID: 0, Quantity: 1}}},
		{"zero quantity", []productionItemRequest{{ProductID: 1, Quantity: 0}}},
		{"negative quantity", []productionItemRequest{{ProductID: 1, Quantity: -1}}},
	}
	for _, c := range cases {
		if _, err := normalizeProductionItems(c.items); err == nil {
			t.Fatalf("[%s] expected error, got nil", c.name)
		}
	}
}
