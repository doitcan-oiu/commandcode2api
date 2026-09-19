package usage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxResponseBytes = 1 << 20

// Error contains a safe, bounded message, never a credential or upstream body.
type Error struct {
	Status  int
	message string
}

func (e *Error) Error() string { return e.message }

type Client struct {
	base  string
	http  *http.Client
	valid bool
}

// NewClient preserves the caller's transport (including HTTP proxy settings),
// but disables redirects so the Authorization header cannot leave the endpoint.
func NewClient(base string, httpClient *http.Client) *Client {
	if base == "" {
		base = "https://api.commandcode.ai"
	}
	base = strings.TrimRight(base, "/")
	u, err := url.Parse(base)
	valid := err == nil && u.Host != "" && (u.Scheme == "http" || u.Scheme == "https") && u.User == nil && u.RawQuery == "" && u.Fragment == ""
	c := http.Client{}
	if httpClient != nil {
		c = *httpClient
	}
	if c.Timeout <= 0 || c.Timeout > 10*time.Second {
		c.Timeout = 10 * time.Second
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	c.Jar = nil
	return &Client{base: base, http: &c, valid: valid}
}

func (c *Client) get(ctx context.Context, path, key string) (map[string]any, error) {
	if !c.valid {
		return nil, &Error{Status: http.StatusBadGateway, message: "invalid usage endpoint"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, &Error{Status: http.StatusBadGateway, message: "invalid usage request"}
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "commandcode2api-usage/1.0")
	resp, err := c.http.Do(req)
	if err != nil {
		message := "usage endpoint request failed"
		if errors.Is(err, context.DeadlineExceeded) {
			message = "usage endpoint request timed out"
		} else if errors.Is(err, context.Canceled) {
			message = "usage endpoint request canceled"
		}
		return nil, &Error{Status: http.StatusBadGateway, message: message}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &Error{Status: resp.StatusCode, message: fmt.Sprintf("upstream returned HTTP %d", resp.StatusCode)}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, &Error{Status: http.StatusBadGateway, message: "usage response read failed"}
	}
	if len(b) > maxResponseBytes {
		return nil, &Error{Status: http.StatusBadGateway, message: "usage response exceeds size limit"}
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var data map[string]any
	if err := dec.Decode(&data); err != nil || data == nil {
		return nil, &Error{Status: http.StatusBadGateway, message: "invalid usage JSON response"}
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return nil, &Error{Status: http.StatusBadGateway, message: "invalid usage JSON response"}
	}
	return data, nil
}

// Fetch returns successful portions when individual data endpoints fail.
// An authentication error at whoami short-circuits the other three requests.
func (c *Client) Fetch(ctx context.Context, key string) (*Report, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	report := &Report{Failures: []string{}}
	fail := func(endpoint string, err error) { report.Failures = append(report.Failures, endpoint+": "+err.Error()) }
	orgID := ""
	who, err := c.get(ctx, "/alpha/whoami", key)
	if err != nil {
		var upstream *Error
		if errors.As(err, &upstream) && (upstream.Status == http.StatusUnauthorized || upstream.Status == http.StatusForbidden) {
			return nil, upstream
		}
		fail("whoami", err)
	} else {
		report.Account, orgID = normalizeAccount(who)
		if report.Account == nil {
			fail("whoami", &Error{message: "unrecognized account response"})
		}
	}
	planID := ""
	cr, err := c.get(ctx, "/alpha/billing/credits", key)
	if err != nil {
		fail("billing/credits", err)
	} else {
		report.Credits, planID = normalizeCredits(cr)
		if report.Credits == nil {
			fail("billing/credits", &Error{message: "unrecognized credits response"})
		}
	}
	subPath := "/alpha/billing/subscriptions"
	if orgID != "" {
		subPath += "?orgId=" + url.QueryEscape(orgID)
	}
	sub, err := c.get(ctx, subPath, key)
	if err != nil {
		fail("billing/subscriptions", err)
	} else {
		report.Plan = normalizePlan(sub, planID)
		if report.Plan == nil {
			fail("billing/subscriptions", &Error{message: "unrecognized subscription response"})
		}
	}
	if report.Plan == nil && planID != "" {
		report.Plan = normalizePlan(nil, planID)
	}
	us, err := c.get(ctx, "/alpha/usage/summary", key)
	if err != nil {
		fail("usage/summary", err)
	} else {
		report.Usage = normalizeSummary(us)
		if report.Usage == nil {
			fail("usage/summary", &Error{message: "unrecognized usage response"})
		}
	}
	if report.Account == nil && report.Credits == nil && report.Plan == nil && report.Usage == nil {
		return report, &Error{Status: http.StatusBadGateway, message: "all usage endpoints failed"}
	}
	return report, nil
}
