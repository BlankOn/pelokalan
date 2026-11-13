package service

import (
	"testing"

	"pelokalan/internal/models"
)

// Known models always use the built-in prices; other models keep the prices entered by hand
func TestModelPrices(t *testing.T) {
	for _, m := range AIModels {
		if pr, ok := AIPrices[m]; !ok || pr.Input <= 0 || pr.Output <= 0 {
			t.Errorf("model %q in the dropdown has no price", m)
		}
	}
	if len(AIPrices) != len(AIModels) {
		t.Errorf("%d prices for %d models", len(AIPrices), len(AIModels))
	}
	st := &models.AISettings{Model: "gpt-5-mini", PriceInput: 9, PriceOutput: 9}
	applyModelPrice(st)
	if st.PriceInput != 0.25 || st.PriceOutput != 2.00 {
		t.Errorf("known model prices: %v / %v", st.PriceInput, st.PriceOutput)
	}
	other := &models.AISettings{Model: "llama-3-70b", PriceInput: 0.5, PriceOutput: 0.7}
	applyModelPrice(other)
	if other.PriceInput != 0.5 || other.PriceOutput != 0.7 {
		t.Error("prices of another model were replaced")
	}
}
