package usage

import (
	"encoding/json"
	"testing"
	"time"
)

func ptr(n float64) *float64 { return &n }

func TestBlockedUntil(t *testing.T) {
	now := time.Date(2026, 9, 19, 1, 0, 0, 0, time.UTC)
	past := now.Add(-time.Minute).UnixMilli()
	future := now.Add(time.Hour).UnixMilli()
	weekly := now.Add(48 * time.Hour).UnixMilli()
	tests := []struct {
		name    string
		report  *Report
		blocked bool
		until   int64
	}{
		{"nil", nil, false, 0},
		{"limited_tier_is_not_exhausted", &Report{Credits: &Credits{Limited: true, FiveHour: &Window{Used: ptr(1), Cap: ptr(5), ResetAt: future}}}, false, 0},
		{"unknown_balance", &Report{Credits: &Credits{MonthlyCredits: ptr(0)}}, false, 0},
		{"estimated_plan_never_blocks", &Report{Plan: &Plan{MonthlyCredits: ptr(0), Estimated: true}}, false, 0},
		{"free_balance_available", &Report{Credits: &Credits{MonthlyCredits: ptr(0), PurchasedCredits: ptr(0), FreeCredits: ptr(1)}}, false, 0},
		{"zero_balance_bounded", &Report{Credits: &Credits{MonthlyCredits: ptr(0), PurchasedCredits: ptr(0), FreeCredits: ptr(0)}}, true, now.Add(5 * time.Minute).UnixMilli()},
		{"derived_window_exceeded", &Report{Credits: &Credits{FiveHour: &Window{Used: ptr(5), Cap: ptr(5), ResetAt: future}}}, true, future},
		{"explicit_window_exceeded", &Report{Credits: &Credits{Weekly: &Window{Exceeded: true, ResetAt: weekly}}}, true, weekly},
		{"both_windows_later_reset", &Report{Credits: &Credits{FiveHour: &Window{Exceeded: true, ResetAt: future}, Weekly: &Window{Exceeded: true, ResetAt: weekly}}}, true, weekly},
		{"global_exceeded_with_window", &Report{Credits: &Credits{Exceeded: "five_hour", FiveHour: &Window{ResetAt: future}}}, true, future},
		{"global_exceeded_missing_window", &Report{Credits: &Credits{Exceeded: "weekly"}}, true, now.Add(5 * time.Minute).UnixMilli()},
		{"global_exceeded_past", &Report{Credits: &Credits{Exceeded: "fiveHour", FiveHour: &Window{Exceeded: true, ResetAt: past}}}, false, 0},
		{"window_past_allows_probe", &Report{Credits: &Credits{FiveHour: &Window{Used: ptr(8), Cap: ptr(5), Exceeded: true, ResetAt: past}}}, false, 0},
		{"generic_flag_uses_exhausted_window", &Report{Credits: &Credits{Exceeded: "true", FiveHour: &Window{Exceeded: true, ResetAt: future}, Weekly: &Window{Used: ptr(1), Cap: ptr(40), ResetAt: weekly}}}, true, future},
		{"generic_flag_expired_window_allows_probe", &Report{Credits: &Credits{Exceeded: "unknown", FiveHour: &Window{Exceeded: true, ResetAt: past}, Weekly: &Window{Used: ptr(1), Cap: ptr(40), ResetAt: weekly}}}, false, 0},
		{"no_reset_bounded", &Report{Credits: &Credits{FiveHour: &Window{Exceeded: true}}}, true, now.Add(5 * time.Minute).UnixMilli()},
		{"false_global_marker", &Report{Credits: &Credits{Exceeded: "false", Limited: true}}, false, 0},
		{"zero_cap_unknown", &Report{Credits: &Credits{FiveHour: &Window{Used: ptr(0), Cap: ptr(0)}}}, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocked, until, reason := tt.report.BlockedUntil(now)
			if blocked != tt.blocked || (blocked && (until.UnixMilli() != tt.until || reason == "")) || (!blocked && (!until.IsZero() || reason != "")) {
				t.Fatalf("got (%v,%v,%q), want blocked=%v until=%d", blocked, until, reason, tt.blocked, tt.until)
			}
		})
	}
}

func TestEpochMillis(t *testing.T) {
	for _, v := range []any{float64(1893456000), float64(1893456000000), json.Number("1893456000"), "1893456000000", "2030-01-01T00:00:00Z", "2030-01-01"} {
		if got := epochMillis(v); got != 1893456000000 {
			t.Errorf("%v -> %d", v, got)
		}
	}
	for _, v := range []any{nil, "garbage", "NaN", "Infinity", float64(-1), json.Number("1e9999")} {
		if got := epochMillis(v); got != 0 {
			t.Errorf("invalid timestamp %v -> %d", v, got)
		}
	}
}
