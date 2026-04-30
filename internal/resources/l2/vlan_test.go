package l2

import (
	"errors"
	"testing"
)

func TestValidateVlanID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		id      int
		wantErr bool
	}{
		{"min", 1, false},
		{"mid", 800, false},
		{"max", 4094, false},
		{"zero", 0, true},
		{"negative", -1, true},
		{"reserved-zero-bound", 4095, true},
		{"way-out", 100000, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateVlanID(tc.id)
			if tc.wantErr && err == nil {
				t.Fatalf("validateVlanID(%d): expected error, got nil", tc.id)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validateVlanID(%d): unexpected %v", tc.id, err)
			}
			if tc.wantErr && !errors.Is(err, ErrVlanIDOutOfRange) {
				t.Fatalf("validateVlanID(%d): got %v, want sentinel ErrVlanIDOutOfRange", tc.id, err)
			}
		})
	}
}

func TestBuildVlanCmds(t *testing.T) {
	t.Parallel()
	name := "prod-frontend"

	tests := []struct {
		name     string
		args     VlanArgs
		remove   bool
		expected []string
	}{
		{
			name:     "create_with_name",
			args:     VlanArgs{Id: 800, Name: &name},
			expected: []string{"vlan 800", "name prod-frontend"},
		},
		{
			name:     "create_no_name",
			args:     VlanArgs{Id: 200},
			expected: []string{"vlan 200"},
		},
		{
			name:     "delete",
			args:     VlanArgs{Id: 999},
			remove:   true,
			expected: []string{"no vlan 999"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := buildVlanCmds(tc.args, tc.remove)
			if len(got) != len(tc.expected) {
				t.Fatalf("len mismatch: got %d (%v), want %d (%v)", len(got), got, len(tc.expected), tc.expected)
			}
			for i := range got {
				if got[i] != tc.expected[i] {
					t.Fatalf("cmd[%d] mismatch: got %q, want %q", i, got[i], tc.expected[i])
				}
			}
		})
	}
}

func TestVlanIDFormatting(t *testing.T) {
	t.Parallel()
	if got := vlanID(800); got != "vlan/800" {
		t.Fatalf("vlanID(800) = %q, want %q", got, "vlan/800")
	}
}
