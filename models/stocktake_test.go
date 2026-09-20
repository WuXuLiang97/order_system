package models

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestDateOnly(t *testing.T) {
	input := time.Date(2026, 9, 20, 15, 42, 31, 123, time.Local)
	got := dateOnly(input)
	if got.Year() != input.Year() || got.Month() != input.Month() || got.Day() != input.Day() {
		t.Fatalf("date changed: got %v, want date %v", got, input)
	}
	if got.Hour() != 0 || got.Minute() != 0 || got.Second() != 0 || got.Nanosecond() != 0 {
		t.Fatalf("time component was not cleared: %v", got)
	}
	if got := stocktakeDateString(input); got != "2026-09-20" {
		t.Fatalf("DATE value changed: got %q, want %q", got, "2026-09-20")
	}
}

func TestValidStocktakeScope(t *testing.T) {
	for _, scope := range []string{StocktakeScopeAll, StocktakeScopeProduct, StocktakeScopeRawMaterial} {
		if !validStocktakeScope(scope) {
			t.Fatalf("expected scope %q to be valid", scope)
		}
	}
	if validStocktakeScope("unknown") {
		t.Fatal("unknown scope should be rejected")
	}
}

func TestValidateStocktakeLine(t *testing.T) {
	validQty := 12.5
	negativeQty := -0.1
	nanQty := math.NaN()
	tooLargeQty := 100000000000.0

	valid := StocktakeLineInput{
		ID: 1, CountedQty: &validQty, Reason: "  损耗  ", Remark: "  已复核  ",
	}
	if err := validateStocktakeLine(&valid, InventoryItemRawMaterial); err != nil {
		t.Fatalf("valid line rejected: %v", err)
	}
	if valid.Reason != "损耗" || valid.Remark != "已复核" {
		t.Fatalf("line text was not normalized: %#v", valid)
	}

	cases := []struct {
		name string
		line StocktakeLineInput
	}{
		{"missing id", StocktakeLineInput{ID: 0}},
		{"negative quantity", StocktakeLineInput{ID: 1, CountedQty: &negativeQty}},
		{"nan quantity", StocktakeLineInput{ID: 1, CountedQty: &nanQty}},
		{"too large quantity", StocktakeLineInput{ID: 1, CountedQty: &tooLargeQty}},
		{"long reason", StocktakeLineInput{ID: 1, Reason: strings.Repeat("原", 301)}},
		{"long remark", StocktakeLineInput{ID: 1, Remark: strings.Repeat("备", 501)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateStocktakeLine(&tc.line, InventoryItemRawMaterial); err == nil {
				t.Fatalf("expected validation error for %s", tc.name)
			}
		})
	}
}

func TestValidateStocktakeProductIntegerQuantity(t *testing.T) {
	fractional := 12.5
	line := StocktakeLineInput{ID: 1, CountedQty: &fractional}
	if err := validateStocktakeLine(&line, InventoryItemProduct); err == nil {
		t.Fatal("fractional product quantity should be rejected")
	}

	whole := 12.0
	line.CountedQty = &whole
	if err := validateStocktakeLine(&line, InventoryItemProduct); err != nil {
		t.Fatalf("whole product quantity rejected: %v", err)
	}
}
