package usage

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
)

func record(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func value(m map[string]any, names ...string) any {
	for _, name := range names {
		if v := m[name]; v != nil {
			return v
		}
	}
	return nil
}

func object(m map[string]any, names ...string) map[string]any {
	for _, name := range names {
		if v := record(m[name]); v != nil {
			return v
		}
	}
	return nil
}

func stringValue(m map[string]any, names ...string) string {
	for _, name := range names {
		if v, ok := m[name].(string); ok {
			return v
		}
	}
	return ""
}

func numeric(v any) *float64 {
	var n float64
	var err error
	switch v := v.(type) {
	case json.Number:
		n, err = v.Float64()
	case float64:
		n = v
	default:
		return nil
	}
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return nil
	}
	return &n
}

func number(m map[string]any, names ...string) *float64 {
	for _, name := range names {
		if n := numeric(m[name]); n != nil {
			return n
		}
	}
	return nil
}

func boolean(v any) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	if s, ok := v.(string); ok {
		return strings.EqualFold(s, "true")
	}
	return false
}

func epochMillis(v any) int64 {
	if s, ok := v.(string); ok {
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02"} {
			if t, err := time.Parse(layout, s); err == nil && t.UnixMilli() > 0 {
				return t.UnixMilli()
			}
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			v = f
		}
	}
	if n := numeric(v); n != nil && *n > 0 {
		ms := *n
		if ms < 1e12 {
			ms *= 1000
		}
		if ms < float64(math.MaxInt64) {
			return int64(ms)
		}
	}
	return 0
}

func normalizeAccount(raw map[string]any) (*Account, string) {
	data := object(raw, "data")
	u := object(raw, "user", "account")
	if u == nil {
		u = object(data, "user", "account")
	}
	org := object(raw, "org", "organization")
	if org == nil {
		org = object(data, "org", "organization")
	}
	orgID := stringValue(org, "id")
	if orgID == "" {
		orgID = stringValue(raw, "orgId", "org_id")
	}
	if orgID == "" {
		orgID = stringValue(data, "orgId", "org_id")
	}
	if u == nil {
		return nil, orgID
	}
	a := &Account{ID: stringValue(u, "id"), Name: stringValue(u, "name"), UserName: stringValue(u, "userName", "username", "user_name")}
	if a.ID == "" && a.Name == "" && a.UserName == "" {
		return nil, orgID
	}
	return a, orgID
}

func normalizeWindow(raw map[string]any) *Window {
	if raw == nil {
		return nil
	}
	w := &Window{
		Used:     number(raw, "used", "usage", "usedCredits", "used_credits"),
		Cap:      number(raw, "cap", "limit", "capCredits", "cap_credits"),
		Exceeded: boolean(value(raw, "exceeded")),
		ResetAt:  epochMillis(value(raw, "resetAt", "reset_at", "resetsAt", "resets_at")),
	}
	if w.Used != nil && w.Cap != nil && *w.Cap > 0 && *w.Used >= *w.Cap {
		w.Exceeded = true
	}
	return w
}

func normalizeCredits(raw map[string]any) (*Credits, string) {
	data := object(raw, "data")
	cr := object(raw, "credits")
	if cr == nil {
		cr = object(data, "credits")
	}
	wl := object(raw, "windowLimits", "window_limits")
	if wl == nil {
		wl = object(data, "windowLimits", "window_limits")
	}
	if len(cr) == 0 && len(wl) == 0 {
		return nil, ""
	}
	exceeded := stringValue(wl, "exceeded")
	if exceeded == "" && boolean(wl["exceeded"]) {
		exceeded = "unknown"
	}
	return &Credits{
		MonthlyCredits:   number(cr, "monthlyCredits", "monthly_credits"),
		PurchasedCredits: number(cr, "purchasedCredits", "purchased_credits"),
		FreeCredits:      number(cr, "freeCredits", "free_credits"),
		Limited:          boolean(wl["limited"]),
		Exceeded:         exceeded,
		BelowThreshold:   boolean(value(cr, "belowThreshold", "below_threshold")),
		CreditThreshold:  number(cr, "creditThreshold", "credit_threshold"),
		FiveHour:         normalizeWindow(object(wl, "fiveHour", "five_hour", "rolling5h", "5h")),
		Weekly:           normalizeWindow(object(wl, "weekly", "week")),
	}, stringValue(cr, "planId", "plan_id")
}

// These community mappings come from commandcode-usage. They are display
// estimates, never authoritative balances or admission-control thresholds.
var knownPlans = []struct {
	id, name string
	credits  float64
}{
	{"individual-provider", "Provider", 15},
	{"individual-pro-v1", "Pro", 80},
	{"individual-ultra", "Ultra", 300},
	{"individual-goat", "GOAT", 70},
	{"individual-max", "Max", 150},
	{"individual-pro", "Pro", 30},
	{"individual-go", "Go", 10},
	{"teams-pro", "Teams Pro", 40},
}

func subscriptionData(raw map[string]any) map[string]any {
	data := object(raw, "data", "subscription")
	if nested := object(data, "subscription"); nested != nil {
		return nested
	}
	if data != nil {
		return data
	}
	// Some API revisions wrap subscriptions in an array. Prefer the active
	// subscription to an older canceled entry while preserving unknown plans.
	for _, key := range []string{"data", "subscriptions"} {
		if items, ok := raw[key].([]any); ok {
			var first map[string]any
			for _, item := range items {
				m := record(item)
				if m == nil {
					continue
				}
				if first == nil {
					first = m
				}
				if stringValue(m, "status") == "active" {
					return m
				}
			}
			return first
		}
	}
	if value(raw, "planId", "plan_id", "status") != nil {
		return raw
	}
	return nil
}

func normalizePlan(raw map[string]any, fallback string) *Plan {
	data := subscriptionData(raw)
	id := stringValue(data, "planId", "plan_id")
	if id == "" {
		id = fallback
	}
	if id == "" && value(data, "status", "monthlyCredits", "monthly_credits", "currentPeriodEnd", "current_period_end", "currentPeriodStart", "current_period_start") == nil {
		return nil
	}
	p := &Plan{
		PlanID:             id,
		Name:               stringValue(data, "name", "planName", "plan_name"),
		Status:             stringValue(data, "status"),
		MonthlyCredits:     number(data, "monthlyCredits", "monthly_credits"),
		CurrentPeriodEnd:   epochMillis(value(data, "currentPeriodEnd", "current_period_end")),
		CurrentPeriodStart: epochMillis(value(data, "currentPeriodStart", "current_period_start")),
		CancelAtPeriodEnd:  boolean(value(data, "cancelAtPeriodEnd", "cancel_at_period_end")),
		PendingPhase:       value(data, "pendingPhase", "pending_phase"),
	}
	norm := strings.ReplaceAll(strings.ToLower(id), "_", "-")
	for _, known := range knownPlans {
		if strings.HasPrefix(norm, known.id) {
			if p.Name == "" {
				p.Name = known.name
			}
			if p.MonthlyCredits == nil {
				p.MonthlyCredits = &known.credits
				p.Estimated = true
			}
			break
		}
	}
	if p.Name == "" {
		p.Name = id
	}
	return p
}

func normalizeSummary(raw map[string]any) *Summary {
	u := object(raw, "data", "usage")
	if u == nil {
		u = raw
	}
	s := &Summary{
		TotalCount:     number(u, "totalCount", "total_count"),
		TotalCost:      number(u, "totalCost", "total_cost"),
		AverageCost:    number(u, "averageCost", "average_cost"),
		SuccessRate:    number(u, "successRate", "success_rate"),
		CompletedCount: number(u, "completedCount", "completed_count"),
		FailedCount:    number(u, "failedCount", "failed_count"),
		TotalTokensIn:  number(u, "totalTokensIn", "total_tokens_in"),
		TotalTokensOut: number(u, "totalTokensOut", "total_tokens_out"),
		TotalCredits:   number(u, "totalCredits", "total_credits"),
		PeriodBasis:    stringValue(u, "periodBasis", "period_basis"),
	}
	for _, n := range []*float64{s.TotalCount, s.TotalCost, s.AverageCost, s.SuccessRate, s.CompletedCount, s.FailedCount, s.TotalTokensIn, s.TotalTokensOut, s.TotalCredits} {
		if n != nil {
			return s
		}
	}
	if s.PeriodBasis != "" {
		return s
	}
	return nil
}
