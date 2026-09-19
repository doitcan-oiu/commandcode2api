// Package usage retrieves account balances and usage from Command Code.
package usage

import (
	"strings"
	"time"
)

// Numeric fields are pointers: an absent upstream value must not look like a
// zero balance. All timestamps are Unix milliseconds; zero means unknown.
type Report struct {
	Account  *Account `json:"account"`
	Credits  *Credits `json:"credits"`
	Plan     *Plan    `json:"plan"`
	Usage    *Summary `json:"usage"`
	Failures []string `json:"failures"`
}

type Account struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	UserName string `json:"userName"`
}

type Credits struct {
	MonthlyCredits   *float64 `json:"monthlyCredits"`
	PurchasedCredits *float64 `json:"purchasedCredits"`
	FreeCredits      *float64 `json:"freeCredits"`
	Limited          bool     `json:"limited"`
	Exceeded         string   `json:"exceeded"`
	BelowThreshold   bool     `json:"belowThreshold"`
	CreditThreshold  *float64 `json:"creditThreshold"`
	FiveHour         *Window  `json:"fiveHour"`
	Weekly           *Window  `json:"weekly"`
}

type Window struct {
	Used     *float64 `json:"used"`
	Cap      *float64 `json:"cap"`
	Exceeded bool     `json:"exceeded"`
	ResetAt  int64    `json:"resetAt"`
}

type Plan struct {
	PlanID             string   `json:"planId"`
	Name               string   `json:"name"`
	Status             string   `json:"status"`
	MonthlyCredits     *float64 `json:"monthlyCredits"`
	CurrentPeriodEnd   int64    `json:"currentPeriodEnd"`
	CurrentPeriodStart int64    `json:"currentPeriodStart"`
	CancelAtPeriodEnd  bool     `json:"cancelAtPeriodEnd"`
	PendingPhase       any      `json:"pendingPhase"`
	Estimated          bool     `json:"estimated"`
}

type Summary struct {
	TotalCount     *float64 `json:"totalCount"`
	TotalCost      *float64 `json:"totalCost"`
	AverageCost    *float64 `json:"averageCost"`
	SuccessRate    *float64 `json:"successRate"`
	CompletedCount *float64 `json:"completedCount"`
	FailedCount    *float64 `json:"failedCount"`
	TotalTokensIn  *float64 `json:"totalTokensIn"`
	TotalTokensOut *float64 `json:"totalTokensOut"`
	TotalCredits   *float64 `json:"totalCredits"`
	PeriodBasis    string   `json:"periodBasis"`
}

// BlockedUntil considers authoritative balances and rolling windows only.
// Estimated plan allocations and the presence of a "limited" tier do not
// establish exhaustion. Unknown reset times get a bounded retry interval so
// a stale snapshot cannot disable an account permanently.
func (r *Report) BlockedUntil(now time.Time) (blocked bool, until time.Time, reason string) {
	if r == nil || r.Credits == nil {
		return false, time.Time{}, ""
	}
	c := r.Credits
	mark := func(reset int64, why string) {
		if reset > 0 && reset <= now.UnixMilli() {
			return // Expired snapshots must allow a new request/refresh.
		}
		end := now.Add(5 * time.Minute)
		if reset > 0 {
			end = time.UnixMilli(reset)
		}
		if !blocked || end.After(until) {
			blocked, until, reason = true, end, why
		}
	}
	global := strings.ToLower(strings.NewReplacer("_", "", "-", "", " ", "").Replace(c.Exceeded))
	matched := false
	windowEvidence := false
	for _, item := range []struct {
		window *Window
		name   string
		global bool
	}{
		{c.FiveHour, "five_hour_exhausted", global == "fivehour" || global == "rolling5h" || global == "5h"},
		{c.Weekly, "weekly_exhausted", global == "weekly" || global == "week"},
	} {
		if item.global {
			matched = true
		}
		w := item.window
		if w == nil {
			if item.global {
				mark(0, item.name)
			}
			continue
		}
		exhausted := w.Exceeded || (w.Used != nil && w.Cap != nil && *w.Cap > 0 && *w.Used >= *w.Cap)
		if exhausted {
			windowEvidence = true
		}
		if item.global || exhausted {
			mark(w.ResetAt, item.name)
		}
	}
	if !matched && !windowEvidence && global != "" && global != "false" && global != "none" {
		// Some revisions expose only an overall exceeded flag. If they also
		// provide reset timestamps, use those rather than a stale global flag.
		reset := int64(0)
		for _, w := range []*Window{c.FiveHour, c.Weekly} {
			if w != nil && w.ResetAt > reset {
				reset = w.ResetAt
			}
		}
		mark(reset, "quota_exhausted")
	}
	if c.MonthlyCredits != nil && c.PurchasedCredits != nil && c.FreeCredits != nil &&
		*c.MonthlyCredits+*c.PurchasedCredits+*c.FreeCredits <= 0 {
		mark(0, "balance_exhausted")
	}
	return
}
