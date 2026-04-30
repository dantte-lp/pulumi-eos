package l2

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Switchport modes accepted by `eos:l2:Interface` and `eos:l2:PortChannel`.
const (
	SwitchportModeAccess = "access"
	SwitchportModeTrunk  = "trunk"
	SwitchportModeRouted = "routed"
)

// Sentinel errors emitted by switchport helpers.
var (
	ErrSwitchportModeInvalid   = errors.New("switchportMode must be access, trunk, or routed")
	ErrSwitchportAccessOnTrunk = errors.New("accessVlan is only valid when switchportMode=access")
	ErrSwitchportTrunkOnAccess = errors.New("trunkAllowedVlans / trunkNativeVlan only valid when switchportMode=trunk")
)

// SwitchportFields is the subset of fields shared between physical
// interfaces and Port-Channels (logical interfaces).
type SwitchportFields struct {
	Mode              *string `pulumi:"switchportMode,optional"`
	AccessVlan        *int    `pulumi:"accessVlan,optional"`
	TrunkAllowedVlans *string `pulumi:"trunkAllowedVlans,optional"`
	TrunkNativeVlan   *int    `pulumi:"trunkNativeVlan,optional"`
}

// validateSwitchport enforces mode-set constraints (access vs trunk).
//
// VLAN-id range checks are delegated to validateVlanID so all callers get
// the same range error and sentinel behaviour.
func validateSwitchport(s SwitchportFields) error {
	mode := ""
	if s.Mode != nil {
		mode = *s.Mode
	}
	switch mode {
	case "", SwitchportModeAccess, SwitchportModeTrunk, SwitchportModeRouted:
	default:
		return fmt.Errorf("%w (got %q)", ErrSwitchportModeInvalid, mode)
	}
	if s.AccessVlan != nil {
		if err := validateVlanID(*s.AccessVlan); err != nil {
			return err
		}
		if mode != "" && mode != SwitchportModeAccess {
			return ErrSwitchportAccessOnTrunk
		}
	}
	if s.TrunkAllowedVlans != nil || s.TrunkNativeVlan != nil {
		if mode != "" && mode != SwitchportModeTrunk {
			return ErrSwitchportTrunkOnAccess
		}
	}
	if s.TrunkNativeVlan != nil {
		if err := validateVlanID(*s.TrunkNativeVlan); err != nil {
			return err
		}
	}
	return nil
}

// buildSwitchportCmds renders the EOS configuration commands for the
// switchport block of an interface or port-channel. The leading `interface
// Xn` line is the caller's responsibility.
func buildSwitchportCmds(s SwitchportFields) []string {
	var cmds []string
	if s.Mode != nil {
		switch *s.Mode {
		case SwitchportModeRouted:
			cmds = append(cmds, "no switchport")
		case SwitchportModeAccess:
			cmds = append(cmds, "switchport", "switchport mode access")
		case SwitchportModeTrunk:
			cmds = append(cmds, "switchport", "switchport mode trunk")
		}
	}
	if s.AccessVlan != nil {
		cmds = append(cmds, "switchport access vlan "+strconv.Itoa(*s.AccessVlan))
	}
	if s.TrunkAllowedVlans != nil && *s.TrunkAllowedVlans != "" {
		cmds = append(cmds, "switchport trunk allowed vlan "+*s.TrunkAllowedVlans)
	}
	if s.TrunkNativeVlan != nil {
		cmds = append(cmds, "switchport trunk native vlan "+strconv.Itoa(*s.TrunkNativeVlan))
	}
	return cmds
}

// switchportRow holds the parsed live state of the switchport block.
//
// Use parseSwitchportLine to populate this from one running-config line at a
// time; see parseInterfaceConfig / parsePortChannelConfig for end-to-end
// usage.
type switchportRow struct {
	Mode              string
	NoSwitchport      bool
	AccessVlan        int
	TrunkAllowedVlans string
	TrunkNativeVlan   int
}

// parseSwitchportLine consumes one trimmed line and updates row when the
// line matches a known switchport directive. Returns true if the line was
// consumed.
func parseSwitchportLine(line string, row *switchportRow) bool {
	switch {
	case line == "no switchport":
		row.NoSwitchport = true
		row.Mode = SwitchportModeRouted
		return true
	case line == "switchport mode access":
		row.Mode = SwitchportModeAccess
		return true
	case line == "switchport mode trunk":
		row.Mode = SwitchportModeTrunk
		return true
	case strings.HasPrefix(line, "switchport access vlan "):
		if v, err := strconv.Atoi(strings.TrimPrefix(line, "switchport access vlan ")); err == nil {
			row.AccessVlan = v
		}
		return true
	case strings.HasPrefix(line, "switchport trunk allowed vlan "):
		row.TrunkAllowedVlans = strings.TrimPrefix(line, "switchport trunk allowed vlan ")
		return true
	case strings.HasPrefix(line, "switchport trunk native vlan "):
		if v, err := strconv.Atoi(strings.TrimPrefix(line, "switchport trunk native vlan ")); err == nil {
			row.TrunkNativeVlan = v
		}
		return true
	}
	return false
}

// fillSwitchport copies parsed switchport row state into the outward-facing
// SwitchportFields struct. Empty zero-values are dropped (Pulumi treats
// nil as "unset").
func fillSwitchport(r switchportRow, s *SwitchportFields) {
	if r.Mode != "" {
		v := r.Mode
		s.Mode = &v
	}
	if r.AccessVlan > 0 {
		v := r.AccessVlan
		s.AccessVlan = &v
	}
	if r.TrunkAllowedVlans != "" {
		v := r.TrunkAllowedVlans
		s.TrunkAllowedVlans = &v
	}
	if r.TrunkNativeVlan > 0 {
		v := r.TrunkNativeVlan
		s.TrunkNativeVlan = &v
	}
}
