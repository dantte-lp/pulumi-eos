package l2

import (
	"errors"
	"testing"
)

func TestValidateMacEntryType(t *testing.T) {
	t.Parallel()
	ok := []string{"dynamic", "static", "all", "DYNAMIC", "Static", "ALL"}
	for _, v := range ok {
		t.Run("ok_"+v, func(t *testing.T) {
			t.Parallel()
			val := v
			if err := validateMacEntryType(&val); err != nil {
				t.Fatalf("validateMacEntryType(%q): %v", v, err)
			}
		})
	}
	if err := validateMacEntryType(nil); err != nil {
		t.Fatalf("validateMacEntryType(nil): %v", err)
	}

	bad := []string{"", "everything", "DYN", "stat"}
	for _, v := range bad {
		t.Run("bad_"+v, func(t *testing.T) {
			t.Parallel()
			val := v
			err := validateMacEntryType(&val)
			if err == nil {
				t.Fatalf("validateMacEntryType(%q): expected error", v)
			}
			if !errors.Is(err, ErrMacAddressTableInvalidEntryType) {
				t.Fatalf("validateMacEntryType(%q): got %v, want sentinel", v, err)
			}
		})
	}
}

func TestParseMacTable_Empty(t *testing.T) {
	t.Parallel()
	resp := []map[string]any{}
	if got := parseMacTable(resp, nil, nil); len(got) != 0 {
		t.Fatalf("expected empty result for empty resp, got %v", got)
	}
}

func TestParseMacTable_Filtering(t *testing.T) {
	t.Parallel()
	resp := []map[string]any{
		{
			"unicastTable": map[string]any{
				"tableEntries": []any{
					map[string]any{
						"vlanId":     float64(100),
						"macAddress": "00:11:22:33:44:55",
						"entryType":  "dynamic",
						"interface":  "Ethernet1",
						"moves":      float64(2),
						"lastMove":   1.7e9,
					},
					map[string]any{
						"vlanId":     float64(100),
						"macAddress": "00:11:22:33:44:66",
						"entryType":  "static",
						"interface":  "Ethernet2",
						"moves":      float64(0),
						"lastMove":   0.0,
					},
					map[string]any{
						"vlanId":     float64(200),
						"macAddress": "AA:BB:CC:DD:EE:FF",
						"entryType":  "dynamic",
						"interface":  "Port-Channel10",
						"moves":      float64(1),
						"lastMove":   1.6e9,
					},
				},
			},
		},
	}

	all := parseMacTable(resp, nil, nil)
	if len(all) != 3 {
		t.Fatalf("no filter: expected 3 rows, got %d (%+v)", len(all), all)
	}
	if all[2].Mac != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("MAC must be lowercased; got %q", all[2].Mac)
	}

	v100 := 100
	byVlan := parseMacTable(resp, &v100, nil)
	if len(byVlan) != 2 {
		t.Fatalf("vlan=100: expected 2 rows, got %d", len(byVlan))
	}
	for _, e := range byVlan {
		if e.Vlan != 100 {
			t.Fatalf("vlan filter leak: %+v", e)
		}
	}

	dyn := "dynamic"
	byType := parseMacTable(resp, nil, &dyn)
	if len(byType) != 2 {
		t.Fatalf("entryType=dynamic: expected 2 rows, got %d", len(byType))
	}
	for _, e := range byType {
		if e.EntryType != "dynamic" {
			t.Fatalf("entryType filter leak: %+v", e)
		}
	}

	static := "STATIC"
	byTypeUpper := parseMacTable(resp, nil, &static)
	if len(byTypeUpper) != 1 {
		t.Fatalf("entryType=STATIC: expected 1 row, got %d", len(byTypeUpper))
	}

	combo := parseMacTable(resp, &v100, &dyn)
	if len(combo) != 1 {
		t.Fatalf("vlan=100,type=dynamic: expected 1 row, got %d", len(combo))
	}
	if combo[0].Mac != "00:11:22:33:44:55" {
		t.Fatalf("combo: wrong row %+v", combo[0])
	}
}

func TestParseMacTable_MissingFields(t *testing.T) {
	t.Parallel()
	resp := []map[string]any{
		{
			"unicastTable": map[string]any{
				"tableEntries": []any{
					map[string]any{},
					"not-a-map",
				},
			},
		},
	}
	got := parseMacTable(resp, nil, nil)
	if len(got) != 1 {
		t.Fatalf("expected 1 row (the empty map; non-map is skipped), got %d", len(got))
	}
	if got[0].Vlan != 0 || got[0].Mac != "" || got[0].EntryType != "" {
		t.Fatalf("missing-field row should default to zero values, got %+v", got[0])
	}
}

func TestParseMacTable_ShapeMismatch(t *testing.T) {
	t.Parallel()
	resp := []map[string]any{
		{"unicastTable": "not-a-map"},
	}
	if got := parseMacTable(resp, nil, nil); len(got) != 0 {
		t.Fatalf("malformed unicastTable: expected empty, got %v", got)
	}

	resp2 := []map[string]any{
		{"unicastTable": map[string]any{"tableEntries": "not-a-list"}},
	}
	if got := parseMacTable(resp2, nil, nil); len(got) != 0 {
		t.Fatalf("malformed tableEntries: expected empty, got %v", got)
	}
}
