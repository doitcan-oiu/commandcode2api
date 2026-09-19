package usage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testKey = "user_test_fixture_never_real"

func usageFixture(t *testing.T, scenario string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testKey {
			t.Error("missing fixture authorization")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/alpha/whoami":
			if scenario == "snake" {
				fmt.Fprint(w, `{"data":{"user":{"id":"u_1","name":"Demo","user_name":"demo"},"org":{"id":"org &/1"}}}`)
			} else {
				fmt.Fprint(w, `{"user":{"id":"u_1","name":"Demo","userName":"demo"},"org":{"id":"org &/1"}}`)
			}
		case "/alpha/billing/credits":
			switch scenario {
			case "snake":
				fmt.Fprint(w, `{"data":{"credits":{"monthly_credits":18.5,"purchased_credits":0,"free_credits":2,"plan_id":"individual_pro_v1_yearly","below_threshold":true,"credit_threshold":1},"window_limits":{"limited":true,"exceeded":"five_hour","rolling5h":{"used_credits":5,"cap_credits":5,"reset_at":1893456000},"week":{"usage":12,"limit":40,"resets_at":"2030-01-02T00:00:00Z"}}}}`)
			case "unknown":
				fmt.Fprint(w, `{"credits":{"monthlyCredits":null,"purchasedCredits":"invalid","planId":"future-plan"},"windowLimits":{"limited":true,"fiveHour":{"resetAt":"bad-date"}}}`)
			case "garbage":
				fmt.Fprint(w, `<html>`+testKey+` upstream private content</html>`)
			default:
				fmt.Fprint(w, `{"credits":{"monthlyCredits":18.5,"purchasedCredits":0,"freeCredits":2,"planId":"individual-goat"},"windowLimits":{"limited":true,"fiveHour":{"used":1.2,"cap":5,"resetAt":1893456000000},"weekly":{"used":12,"cap":40,"resetAt":"2030-01-02T00:00:00Z"}}}`)
			}
		case "/alpha/billing/subscriptions":
			if r.URL.Query().Get("orgId") != "org &/1" {
				t.Errorf("orgId not URL-encoded correctly: %q", r.URL.RawQuery)
			}
			switch scenario {
			case "partial":
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, `{"private":"`+testKey+`"}`)
			case "snake":
				fmt.Fprint(w, `{"subscription":{"plan_id":"individual_pro_v1_yearly","status":"active","current_period_start":1890864000,"current_period_end":"2030-02-01T00:00:00Z","cancel_at_period_end":true,"pending_phase":{"planId":"individual-go"}}}`)
			case "unknown":
				fmt.Fprint(w, `{"data":{"planId":"future-plan","status":"active"}}`)
			default:
				fmt.Fprint(w, `{"data":{"planId":"individual-goat","status":"active","currentPeriodStart":1890864000000,"currentPeriodEnd":"2030-02-01T00:00:00Z","cancelAtPeriodEnd":false,"pendingPhase":null}}`)
			}
		case "/alpha/usage/summary":
			if scenario == "partial" {
				w.WriteHeader(http.StatusServiceUnavailable)
				fmt.Fprint(w, testKey+" upstream private content")
			} else if scenario == "snake" {
				fmt.Fprint(w, `{"data":{"total_count":1883,"total_cost":6.25,"average_cost":0.003,"success_rate":99,"completed_count":1880,"failed_count":3,"total_tokens_in":2000,"total_tokens_out":1000,"total_credits":6.25,"period_basis":"billing-period"}}`)
			} else if scenario == "unknown" {
				fmt.Fprint(w, `{"totalCount":1,"totalCost":null}`)
			} else {
				fmt.Fprint(w, `{"totalCount":1883,"totalCost":6.25,"averageCost":0.003,"successRate":99,"completedCount":1880,"failedCount":3,"totalTokensIn":2000,"totalTokensOut":1000,"totalCredits":6.25,"periodBasis":"billing-period"}`)
			}
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestFetchNormalizedReports(t *testing.T) {
	for _, scenario := range []string{"normal", "snake", "partial", "unknown", "garbage"} {
		t.Run(scenario, func(t *testing.T) {
			srv := usageFixture(t, scenario)
			defer srv.Close()
			report, err := NewClient(srv.URL, srv.Client()).Fetch(context.Background(), testKey)
			if err != nil {
				t.Fatal(err)
			}
			if report.Account == nil || report.Account.UserName != "demo" {
				t.Fatalf("unexpected account: %+v", report.Account)
			}
			if report.Plan == nil {
				t.Fatal("missing plan")
			}
			encoded, err := json.Marshal(report)
			if err != nil || strings.Contains(string(encoded), testKey) || strings.Contains(string(encoded), "private content") {
				t.Fatalf("report contains a secret or is invalid: %v", err)
			}
			switch scenario {
			case "normal", "snake":
				if len(report.Failures) != 0 || report.Credits == nil || report.Usage == nil {
					t.Fatalf("incomplete report: %+v", report)
				}
				if report.Credits.FiveHour.ResetAt != 1893456000000 || report.Credits.Weekly.ResetAt != 1893542400000 {
					t.Fatalf("timestamps not normalized: %+v", report.Credits)
				}
				if *report.Usage.TotalCount != 1883 || *report.Usage.TotalTokensOut != 1000 {
					t.Fatalf("invalid usage: %+v", report.Usage)
				}
				wantCredits := 70.0
				if scenario == "snake" {
					wantCredits = 80
					if !report.Credits.FiveHour.Exceeded || !report.Plan.CancelAtPeriodEnd || report.Plan.PendingPhase == nil {
						t.Fatal("snake_case state fields lost")
					}
				}
				if !report.Plan.Estimated || report.Plan.MonthlyCredits == nil || *report.Plan.MonthlyCredits != wantCredits {
					t.Fatalf("bad estimate: %+v", report.Plan)
				}
				if report.Plan.CurrentPeriodEnd != 1896134400000 || report.Plan.CurrentPeriodStart != 1890864000000 {
					t.Fatalf("bad plan timestamps: %+v", report.Plan)
				}
			case "partial":
				if len(report.Failures) != 2 || report.Usage != nil || report.Credits == nil || report.Plan.PlanID != "individual-goat" {
					t.Fatalf("failed to preserve partial result: %+v", report)
				}
			case "unknown":
				if report.Credits.MonthlyCredits != nil || report.Credits.PurchasedCredits != nil || report.Credits.FreeCredits != nil || report.Credits.FiveHour.Used != nil || report.Credits.FiveHour.Cap != nil || report.Plan.MonthlyCredits != nil || report.Plan.Estimated || report.Usage.TotalCost != nil || report.Usage.TotalCredits != nil {
					t.Fatal("unknown numeric values converted to zero")
				}
				if !strings.Contains(string(encoded), `"monthlyCredits":null`) || !strings.Contains(string(encoded), `"totalCredits":null`) {
					t.Fatal("unknown values not serialized as null")
				}
			case "garbage":
				if report.Credits != nil || len(report.Failures) != 1 || !strings.Contains(report.Failures[0], "invalid usage JSON") {
					t.Fatalf("invalid JSON not isolated: %+v", report)
				}
			}
		})
	}
}

func TestWhoamiAuthenticationShortCircuit(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(status)
				fmt.Fprint(w, testKey)
			}))
			defer srv.Close()
			report, err := NewClient(srv.URL, nil).Fetch(context.Background(), testKey)
			var authErr *Error
			if report != nil || !errors.As(err, &authErr) || authErr.Status != status || calls.Load() != 1 || strings.Contains(err.Error(), testKey) {
				t.Fatalf("authentication did not short circuit safely: calls=%d, err=%v", calls.Load(), err)
			}
		})
	}
}

func TestRedirectDoesNotForwardCredentials(t *testing.T) {
	var destinationCalls atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { destinationCalls.Add(1) }))
	defer destination.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	client := srv.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		t.Error("caller redirect policy should be overridden on the copy")
		return nil
	}
	report, err := NewClient(srv.URL, client).Fetch(context.Background(), testKey)
	if err == nil || report == nil || len(report.Failures) != 4 || destinationCalls.Load() != 0 {
		t.Fatalf("redirect unexpectedly followed: calls=%d err=%v", destinationCalls.Load(), err)
	}
	if client.CheckRedirect == nil || client.Timeout != 0 {
		t.Fatal("caller client was modified")
	}
}

func TestResponseBoundAndInvalidJSON(t *testing.T) {
	for _, body := range []string{strings.Repeat(" ", maxResponseBytes+1), `{"ok":true} {"trailing":true}`, `null`, `[]`, `{}`, `{"data":{}}`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		report, err := NewClient(srv.URL, nil).Fetch(context.Background(), testKey)
		srv.Close()
		if err == nil || report == nil || len(report.Failures) != 4 {
			t.Fatalf("invalid response accepted (size %d): %v %+v", len(body), err, report)
		}
	}
}

func TestFetchCanceledContext(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewClient(srv.URL, nil).Fetch(ctx, testKey)
	if err == nil || calls.Load() != 0 {
		t.Fatalf("canceled context sent requests: %d, %v", calls.Load(), err)
	}
}

func TestClientTimeoutIsBoundedAndPreservesTransport(t *testing.T) {
	transport := &http.Transport{}
	original := &http.Client{Transport: transport, Timeout: time.Minute}
	c := NewClient("https://commandcode.ai", original)
	if c.http.Transport != transport || c.http.Timeout != 10*time.Second || original.Timeout != time.Minute {
		t.Fatal("transport or caller timeout not preserved")
	}
	short := NewClient("https://commandcode.ai", &http.Client{Timeout: time.Second})
	if short.http.Timeout != time.Second {
		t.Fatal("shorter caller timeout ignored")
	}
	for _, base := range []string{"http://user:secret@example.test", "file:///tmp/report", "https://example.test?secret=key", "https://example.test#fragment"} {
		_, err := NewClient(base, original).Fetch(context.Background(), testKey)
		if err == nil || strings.Contains(err.Error(), base) {
			t.Fatal("invalid base URL was not safely rejected")
		}
	}
}

func TestPlanPrefersActiveAndAuthoritativeAllowance(t *testing.T) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(`{"data":[{"planId":"old","status":"canceled"},{"planId":"individual-goat","status":"active","monthlyCredits":90}]}`), &raw); err != nil {
		t.Fatal(err)
	}
	p := normalizePlan(raw, "")
	if p.PlanID != "individual-goat" || p.MonthlyCredits == nil || *p.MonthlyCredits != 90 || p.Estimated {
		t.Fatalf("bad active plan selection: %+v", p)
	}
}
