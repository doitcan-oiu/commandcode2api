package gateway

import (
	"commandcode2api/internal/usage"
	"net/http"
	"sync"
	"time"
)

type Options struct {
	DataDir       string
	AdminPassword string
	UsageClient   *usage.Client
}
type Error struct {
	Status     int
	Message    string
	RetryAfter int
}

func (e *Error) Error() string { return e.Message }

type Settings struct {
	Strategy               string `json:"strategy"`
	MaxRetries             int    `json:"maxRetries"`
	RefreshIntervalSeconds int    `json:"refreshIntervalSeconds"`
	CooldownSeconds        int    `json:"cooldownSeconds"`
	LogRetentionDays       int    `json:"logRetentionDays"`
	SessionAffinity        bool   `json:"sessionAffinity"`
}

func defaultSettings() Settings {
	return Settings{Strategy: "quota_aware", MaxRetries: 2, RefreshIntervalSeconds: 300, CooldownSeconds: 60, LogRetentionDays: 30, SessionAffinity: true}
}

type Account struct {
	ID            string        `json:"id"`
	Label         string        `json:"label"`
	KeyPreview    string        `json:"keyPreview"`
	Enabled       bool          `json:"enabled"`
	Weight        int           `json:"weight"`
	Priority      int           `json:"priority"`
	MaxConcurrent int           `json:"maxConcurrent"`
	Models        []string      `json:"models"`
	Status        string        `json:"status"`
	CooldownUntil int64         `json:"cooldownUntil"`
	LastError     string        `json:"lastError"`
	Inflight      int           `json:"inflight"`
	Usage         *usage.Report `json:"usage"`
	LastRefreshAt int64         `json:"lastRefreshAt"`
	CreatedAt     int64         `json:"createdAt"`
	UpdatedAt     int64         `json:"updatedAt"`
}
type Client struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	TokenPreview  string   `json:"tokenPreview"`
	Enabled       bool     `json:"enabled"`
	RPM           int      `json:"rpm"`
	MaxConcurrent int      `json:"maxConcurrent"`
	MaxRequests   int64    `json:"maxRequests"`
	MaxTokens     int64    `json:"maxTokens"`
	Models        []string `json:"models"`
	ExpiresAt     int64    `json:"expiresAt"`
	UsedRequests  int64    `json:"usedRequests"`
	UsedTokens    int64    `json:"usedTokens"`
	Inflight      int      `json:"inflight"`
	LastUsedAt    int64    `json:"lastUsedAt"`
	CreatedAt     int64    `json:"createdAt"`
}
type Record struct {
	ID           string `json:"id"`
	Model        string `json:"model"`
	Protocol     string `json:"protocol"`
	AccountID    string `json:"accountId"`
	AccountName  string `json:"accountName"`
	ClientID     string `json:"clientId"`
	ClientName   string `json:"clientName"`
	Status       int    `json:"status"`
	Stream       bool   `json:"stream"`
	InputTokens  int64  `json:"inputTokens"`
	OutputTokens int64  `json:"outputTokens"`
	LatencyMS    int64  `json:"latencyMs"`
	Attempts     int    `json:"attempts"`
	Error        string `json:"error"`
	CreatedAt    int64  `json:"createdAt"`
}
type Permit struct {
	ClientID, ClientName string
	manager              *Manager
	once                 sync.Once
}

func (p *Permit) Finish(r Record) {
	if p != nil {
		p.once.Do(func() { p.manager.finishPermit(p, r) })
	}
}

type Lease struct {
	AccountID, AccountName, Key string
	manager                     *Manager
	once                        sync.Once
}

func (l *Lease) Finish(status int, retryAfter time.Duration) {
	if l != nil {
		l.once.Do(func() { l.manager.finishLease(l, status, retryAfter) })
	}
}

type Stats struct {
	TotalAccounts    int     `json:"totalAccounts"`
	ActiveAccounts   int     `json:"activeAccounts"`
	TotalClients     int     `json:"totalClients"`
	Inflight         int     `json:"inflight"`
	Requests24h      int64   `json:"requests24h"`
	SuccessRate      float64 `json:"successRate"`
	InputTokens24h   int64   `json:"inputTokens24h"`
	OutputTokens24h  int64   `json:"outputTokens24h"`
	AverageLatencyMs float64 `json:"averageLatencyMs"`
}
type SeriesPoint struct {
	Timestamp int64 `json:"timestamp"`
	Requests  int64 `json:"requests"`
	Errors    int64 `json:"errors"`
	Tokens    int64 `json:"tokens"`
}
type Overview struct {
	Stats      Stats         `json:"stats"`
	Series     []SeriesPoint `json:"series"`
	RecentLogs []Record      `json:"recentLogs"`
}

func apiError(status int, message string) *Error { return &Error{Status: status, Message: message} }
func unavailable(message string) *Error {
	return &Error{Status: http.StatusServiceUnavailable, Message: message, RetryAfter: 5}
}
