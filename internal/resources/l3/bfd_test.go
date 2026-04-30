package l3

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateBfd(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		args    BfdArgs
		wantErr error
	}{
		{"empty", BfdArgs{}, nil},
		{
			name: "all_three_ok",
			args: BfdArgs{Interval: new(300), MinRx: new(300), Multiplier: new(3)},
		},
		{
			name:    "incomplete_only_interval",
			args:    BfdArgs{Interval: new(300)},
			wantErr: ErrBfdTimerBundleIncomplete,
		},
		{
			name:    "incomplete_interval_minrx",
			args:    BfdArgs{Interval: new(300), MinRx: new(300)},
			wantErr: ErrBfdTimerBundleIncomplete,
		},
		{
			name:    "interval_zero",
			args:    BfdArgs{Interval: new(0), MinRx: new(300), Multiplier: new(3)},
			wantErr: ErrBfdIntervalNonPositive,
		},
		{
			name:    "minrx_negative",
			args:    BfdArgs{Interval: new(300), MinRx: new(-1), Multiplier: new(3)},
			wantErr: ErrBfdIntervalNonPositive,
		},
		{
			name:    "multiplier_too_low",
			args:    BfdArgs{Interval: new(300), MinRx: new(300), Multiplier: new(2)},
			wantErr: ErrBfdMultiplierOutOfRange,
		},
		{
			name:    "multiplier_too_high",
			args:    BfdArgs{Interval: new(300), MinRx: new(300), Multiplier: new(51)},
			wantErr: ErrBfdMultiplierOutOfRange,
		},
		{
			name:    "slowtimer_zero",
			args:    BfdArgs{SlowTimer: new(0)},
			wantErr: ErrBfdSlowTimerNonPositive,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateBfd(tc.args)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("unexpected: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestBuildBfdCmds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		args  BfdArgs
		reset bool
		want  []string
	}{
		{
			name: "timers_only",
			args: BfdArgs{Interval: new(300), MinRx: new(300), Multiplier: new(3)},
			want: []string{
				"router bfd",
				"interval 300 min-rx 300 multiplier 3 default",
				"exit",
			},
		},
		{
			name: "full_block_with_v6_overlay_defaults",
			args: BfdArgs{
				Interval:   new(100),
				MinRx:      new(100),
				Multiplier: new(3),
				SlowTimer:  new(2000),
				Shutdown:   new(false),
			},
			want: []string{
				"router bfd",
				"interval 100 min-rx 100 multiplier 3 default",
				"slow-timer 2000",
				"no shutdown",
				"exit",
			},
		},
		{
			name: "shutdown_only",
			args: BfdArgs{Shutdown: new(true)},
			want: []string{"router bfd", "shutdown", "exit"},
		},
		{
			name:  "reset",
			reset: true,
			want:  []string{"no router bfd"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := buildBfdCmds(tc.args, tc.reset)
			if len(got) != len(tc.want) {
				t.Fatalf("len mismatch: got %d (%v), want %d (%v)", len(got), got, len(tc.want), tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("cmd[%d]: got %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestBfdID(t *testing.T) {
	t.Parallel()
	if got := bfdID(); got != "bfd/global" {
		t.Fatalf("got %q, want bfd/global", got)
	}
}

func TestParseBfdSection(t *testing.T) {
	t.Parallel()
	out := strings.Join([]string{
		"router bfd",
		"   interval 100 min-rx 100 multiplier 3",
		"   slow-timer 2000",
		"   shutdown",
		"!",
	}, "\n")
	row := parseBfdSection(out)
	if !row.HasTimers {
		t.Fatal("HasTimers should be true")
	}
	if row.Interval != 100 || row.MinRx != 100 || row.Multiplier != 3 {
		t.Fatalf("timers = %+v, want 100/100/3", row)
	}
	if row.SlowTimer != 2000 {
		t.Fatalf("slowTimer = %d, want 2000", row.SlowTimer)
	}
	if !row.Shutdown {
		t.Fatal("shutdown should be true")
	}
}

func TestParseBfdSection_Empty(t *testing.T) {
	t.Parallel()
	row := parseBfdSection("")
	if row.HasTimers || row.Shutdown || row.SlowTimer != 0 {
		t.Fatalf("empty parse should produce zero row, got %+v", row)
	}
}
