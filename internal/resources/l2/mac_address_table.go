package l2

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/pulumi/pulumi-go-provider/infer"

	"github.com/dantte-lp/pulumi-eos/internal/config"
)

// Sentinel errors returned by the MacAddressTable function.
var (
	ErrMacAddressTableInvalidEntryType = errors.New("entryType must be one of: dynamic, static, all")
)

// MacAddressTable is a read-only data source that returns the unicast MAC
// address-table from an EOS device, optionally filtered by VLAN and entry
// type.
//
// Unlike a Pulumi resource, an `infer.Function` has no Pulumi state — every
// call hits the device. The function is exposed under the token
// `eos:l2:macAddressTable` (functions follow lowerCamel by Pulumi
// convention).
//
// Source: EOS Command API Guide §6 — `show mac address-table` JSON schema.
type MacAddressTable struct{}

// MacAddressTableArgs is the input filter set.
type MacAddressTableArgs struct {
	// Vlan optionally restricts the returned rows to a single VLAN id.
	Vlan *int `pulumi:"vlan,optional"`
	// EntryType optionally filters by entry type. Accepted values:
	// "dynamic", "static", "all" (default). Anything else is rejected.
	EntryType *string `pulumi:"entryType,optional"`

	// Host overrides the provider-level eosUrl host for this call.
	Host *string `pulumi:"host,optional"`
	// Username overrides the provider-level eosUsername for this call.
	Username *string `pulumi:"username,optional"`
	// Password overrides the provider-level eosPassword for this call.
	Password *string `provider:"secret" pulumi:"password,optional"`
}

// MacAddressTableResult wraps the unicast table contents.
type MacAddressTableResult struct {
	// Entries is the list of MAC entries matching the input filters.
	Entries []MacAddressEntry `pulumi:"entries"`
	// Count is len(Entries) exposed for convenience.
	Count int `pulumi:"count"`
}

// MacAddressEntry is one row of the unicast MAC table.
type MacAddressEntry struct {
	// Vlan is the VLAN id the MAC was learned in.
	Vlan int `pulumi:"vlan"`
	// Mac is the MAC address in EOS-canonical lowercase colon form.
	Mac string `pulumi:"mac"`
	// EntryType is "dynamic" or "static".
	EntryType string `pulumi:"entryType"`
	// Interface is the EOS interface name where the MAC was learned.
	Interface string `pulumi:"interface"`
	// Moves is the cumulative move count recorded by EOS.
	Moves int `pulumi:"moves"`
	// LastMove is the EOS-reported epoch timestamp of the last move
	// (seconds, with fractional precision).
	LastMove float64 `pulumi:"lastMove"`
}

// Annotate documents the function and its filter knobs.
func (f *MacAddressTable) Annotate(a infer.Annotator) {
	a.Describe(&f, "Read-only data source: returns the unicast MAC address-table from an EOS device, optionally filtered by VLAN and entry type.")
}

// Annotate documents the input filter fields.
func (a *MacAddressTableArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Vlan, "Restrict rows to a single VLAN id (1..4094). Omit to return all VLANs.")
	an.Describe(&a.EntryType, "Restrict rows by entry type. Accepts \"dynamic\", \"static\", or \"all\" (default).")
	an.Describe(&a.Host, "Optional management hostname override.")
	an.Describe(&a.Username, "Optional AAA username override.")
	an.Describe(&a.Password, "Optional AAA password override.")
}

// Annotate documents the result fields.
func (r *MacAddressTableResult) Annotate(an infer.Annotator) {
	an.Describe(&r.Entries, "Matching MAC table rows.")
	an.Describe(&r.Count, "Length of `entries`, exposed for convenience.")
}

// Annotate documents the per-entry fields.
func (e *MacAddressEntry) Annotate(an infer.Annotator) {
	an.Describe(&e.Vlan, "VLAN id the MAC was learned in.")
	an.Describe(&e.Mac, "MAC address in EOS-canonical lowercase colon form.")
	an.Describe(&e.EntryType, "Entry type (\"dynamic\" or \"static\").")
	an.Describe(&e.Interface, "EOS interface name where the MAC was learned.")
	an.Describe(&e.Moves, "Cumulative move count recorded by EOS.")
	an.Describe(&e.LastMove, "Epoch timestamp of the last move (seconds, fractional).")
}

// Invoke runs `show mac address-table` and applies optional filters.
func (*MacAddressTable) Invoke(ctx context.Context, req infer.FunctionRequest[MacAddressTableArgs]) (infer.FunctionResponse[MacAddressTableResult], error) {
	if err := validateMacEntryType(req.Input.EntryType); err != nil {
		return infer.FunctionResponse[MacAddressTableResult]{}, err
	}
	cfg := config.FromContext(ctx)
	cli, err := cfg.EAPIClient(ctx, req.Input.Host, req.Input.Username, req.Input.Password)
	if err != nil {
		return infer.FunctionResponse[MacAddressTableResult]{}, err
	}
	resp, err := cli.RunCmds(ctx, []string{"show mac address-table"}, "json")
	if err != nil {
		return infer.FunctionResponse[MacAddressTableResult]{}, fmt.Errorf("show mac address-table: %w", err)
	}
	entries := parseMacTable(resp, req.Input.Vlan, req.Input.EntryType)
	return infer.FunctionResponse[MacAddressTableResult]{
		Output: MacAddressTableResult{Entries: entries, Count: len(entries)},
	}, nil
}

// validateMacEntryType returns nil for "dynamic", "static", "all", and the
// nil pointer (omitted = "all").
func validateMacEntryType(t *string) error {
	if t == nil {
		return nil
	}
	switch strings.ToLower(*t) {
	case "dynamic", "static", "all":
		return nil
	}
	return fmt.Errorf("%w: got %q", ErrMacAddressTableInvalidEntryType, *t)
}

// parseMacTable extracts the unicastTable.tableEntries from the EOS JSON
// response and applies VLAN / entry-type filters.
func parseMacTable(resp []map[string]any, vlan *int, entryType *string) []MacAddressEntry {
	out := make([]MacAddressEntry, 0)
	if len(resp) == 0 {
		return out
	}
	uni, ok := resp[0]["unicastTable"].(map[string]any)
	if !ok {
		return out
	}
	rows, ok := uni["tableEntries"].([]any)
	if !ok {
		return out
	}
	wantType := "all"
	if entryType != nil {
		wantType = strings.ToLower(*entryType)
	}
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		entry := macEntryFromRow(row)
		if vlan != nil && entry.Vlan != *vlan {
			continue
		}
		if wantType != "all" && entry.EntryType != wantType {
			continue
		}
		out = append(out, entry)
	}
	return out
}

// macEntryFromRow projects an EOS JSON row into MacAddressEntry. Missing
// fields default to zero values; the EOS schema guarantees the fields
// present when the table is non-empty.
func macEntryFromRow(row map[string]any) MacAddressEntry {
	entry := MacAddressEntry{}
	if v, ok := row["vlanId"].(float64); ok {
		entry.Vlan = int(v)
	}
	if v, ok := row["macAddress"].(string); ok {
		entry.Mac = strings.ToLower(v)
	}
	if v, ok := row["entryType"].(string); ok {
		entry.EntryType = strings.ToLower(v)
	}
	if v, ok := row["interface"].(string); ok {
		entry.Interface = v
	}
	if v, ok := row["moves"].(float64); ok {
		entry.Moves = int(v)
	}
	if v, ok := row["lastMove"].(float64); ok {
		entry.LastMove = v
	}
	return entry
}
