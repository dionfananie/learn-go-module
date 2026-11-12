package events

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestStockChangedJSONFieldNames(t *testing.T) {
	ev := StockChanged{
		ProductID: 7, UserID: "u1", Action: "subtract",
		OldStock: 10, NewStock: 5, AuditID: 42,
		At: time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC),
	}

	raw, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}

	// Nama field harus snake_case (kontrak wire format dengan consumer).
	for _, key := range []string{"product_id", "user_id", "action", "old_stock", "new_stock", "audit_id", "at"} {
		if _, ok := m[key]; !ok {
			t.Fatalf("field JSON %q hilang: %s", key, raw)
		}
	}
}

func TestStockChangedAuditIDOmittedWhenZero(t *testing.T) {
	ev := StockChanged{ProductID: 1, Action: "transfer_out", OldStock: 10, NewStock: 8}
	raw, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "audit_id") {
		t.Fatalf("audit_id=0 seharusnya di-omit (omitempty): %s", raw)
	}
}
