package gateway

import (
	"commandcode2api/internal/usage"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type storedAccount struct {
	Account
	EncryptedKey  string `json:"encryptedKey"`
	KeyHash       string `json:"keyHash"`
	score         float64
	healthVersion uint64
}
type storedClient struct {
	Client
	TokenHash      string `json:"tokenHash"`
	windowStart    time.Time
	windowRequests int
}
type admin struct {
	Username     string `json:"username"`
	PasswordHash string `json:"passwordHash"`
}
type session struct{ Expires time.Time }
type affinity struct {
	AccountID string
	Expires   time.Time
}
type attempt struct {
	Count int
	Since time.Time
}
type refreshCall struct {
	done    chan struct{}
	account Account
	err     error
}
type Manager struct {
	mu            sync.Mutex
	db            *sql.DB
	cipher        cipher.AEAD
	dataDir       string
	usage         *usage.Client
	accounts      map[string]*storedAccount
	clients       map[string]*storedClient
	sessions      map[string]session
	affinity      map[string]affinity
	loginAttempts map[string]attempt
	settings      Settings
	admin         admin
	refreshMu     sync.Mutex
	refreshJobs   map[string]*refreshCall
}

func Open(o Options) (*Manager, error) {
	if o.DataDir == "" {
		o.DataDir = "data"
	}
	if err := os.MkdirAll(o.DataDir, 0700); err != nil {
		return nil, err
	}
	keyPath := filepath.Join(o.DataDir, "master.key")
	key, err := os.ReadFile(keyPath)
	if errors.Is(err, os.ErrNotExist) {
		if info, e := os.Stat(filepath.Join(o.DataDir, "gateway.db")); e == nil && info.Size() > 0 {
			return nil, errors.New("master.key is missing for existing database; restore the original encryption key")
		}
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		if err = writeSecret(keyPath, key); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, errors.New("master.key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(o.DataDir, "gateway.db"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	ok := false
	defer func() {
		if !ok {
			db.Close()
		}
	}()
	for _, query := range []string{"PRAGMA journal_mode=WAL", "PRAGMA busy_timeout=5000", "PRAGMA foreign_keys=ON", `CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`, `CREATE TABLE IF NOT EXISTS accounts (id TEXT PRIMARY KEY, value TEXT NOT NULL)`, `CREATE TABLE IF NOT EXISTS clients (id TEXT PRIMARY KEY, value TEXT NOT NULL)`, `CREATE TABLE IF NOT EXISTS logs (id TEXT PRIMARY KEY, created_at INTEGER NOT NULL, status INTEGER NOT NULL, model TEXT NOT NULL, client_id TEXT NOT NULL, account_id TEXT NOT NULL, value TEXT NOT NULL)`, `CREATE INDEX IF NOT EXISTS logs_time ON logs(created_at DESC)`, `CREATE INDEX IF NOT EXISTS logs_client ON logs(client_id,created_at DESC)`, `PRAGMA user_version=1`} {
		if _, err = db.Exec(query); err != nil {
			return nil, err
		}
	}
	_ = os.Chmod(filepath.Join(o.DataDir, "gateway.db"), 0600)
	m := &Manager{db: db, cipher: aead, dataDir: o.DataDir, usage: o.UsageClient, accounts: map[string]*storedAccount{}, clients: map[string]*storedClient{}, sessions: map[string]session{}, affinity: map[string]affinity{}, loginAttempts: map[string]attempt{}, refreshJobs: map[string]*refreshCall{}, settings: defaultSettings()}
	if m.usage == nil {
		m.usage = usage.NewClient("", nil)
	}
	if err = m.load(); err != nil {
		return nil, err
	}
	if m.admin.Username == "" && o.AdminPassword != "" {
		if !validPassword(o.AdminPassword) {
			return nil, errors.New("CC_ADMIN_PASSWORD must contain 8–1024 characters")
		}
		m.admin = admin{Username: "admin", PasswordHash: hashPassword(o.AdminPassword)}
		if err = m.saveMeta("admin", m.admin); err != nil {
			return nil, err
		}
	}
	// Older releases created this file; initialization no longer uses it.
	_ = os.Remove(filepath.Join(o.DataDir, "setup.token"))
	ok = true
	return m, nil
}
func writeSecret(path string, b []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
func (m *Manager) load() error {
	for _, table := range []string{"accounts", "clients", "meta"} {
		col := "id"
		if table == "meta" {
			col = "key"
		}
		rows, err := m.db.Query("SELECT " + col + ",value FROM " + table)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id, v string
			if err = rows.Scan(&id, &v); err != nil {
				rows.Close()
				return err
			}
			switch table {
			case "accounts":
				a := &storedAccount{}
				err = json.Unmarshal([]byte(v), a)
				if err == nil {
					_, err = m.decrypt(a.EncryptedKey)
				}
				a.Inflight = 0
				m.accounts[id] = a
			case "clients":
				c := &storedClient{}
				err = json.Unmarshal([]byte(v), c)
				c.Inflight = 0
				m.clients[id] = c
			case "meta":
				if id == "settings" {
					err = json.Unmarshal([]byte(v), &m.settings)
				}
				if id == "admin" {
					err = json.Unmarshal([]byte(v), &m.admin)
				}
			}
			if err != nil {
				rows.Close()
				return fmt.Errorf("load %s: %w", table, err)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
func (m *Manager) saveMeta(key string, v any) error { return m.save("meta", "key", key, v) }
func (m *Manager) save(table, col, id string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = m.db.Exec("INSERT INTO "+table+" ("+col+",value) VALUES (?,?) ON CONFLICT("+col+") DO UPDATE SET value=excluded.value", id, string(b))
	return err
}
func (m *Manager) saveAccount(a *storedAccount) error { return m.save("accounts", "id", a.ID, a) }
func (m *Manager) saveClient(c *storedClient) error   { return m.save("clients", "id", c.ID, c) }
func (m *Manager) Close() error                       { return m.db.Close() }
func (m *Manager) SetupRequired() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.admin.Username == ""
}
func (m *Manager) Settings() Settings { m.mu.Lock(); defer m.mu.Unlock(); return m.settings }
func (m *Manager) encrypt(key string) string {
	nonce := make([]byte, m.cipher.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic(err)
	}
	return base64.RawStdEncoding.EncodeToString(m.cipher.Seal(nonce, nonce, []byte(key), []byte("commandcode-gateway-key-v1")))
}
func (m *Manager) decrypt(s string) (string, error) {
	b, e := base64.RawStdEncoding.DecodeString(s)
	if e != nil || len(b) < m.cipher.NonceSize() {
		return "", errors.New("invalid encrypted key")
	}
	n := m.cipher.NonceSize()
	p, e := m.cipher.Open(nil, b[:n], b[n:], []byte("commandcode-gateway-key-v1"))
	return string(p), e
}
func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func id(prefix string) string { return prefix + randomToken(12) }
func digest(s string) string  { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }
func (m *Manager) accountListLocked() []Account {
	out := make([]Account, 0, len(m.accounts))
	for _, a := range m.accounts {
		out = append(out, publicAccount(a))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out
}
func publicAccount(a *storedAccount) Account {
	out := a.Account
	if !out.Enabled {
		out.Status = "disabled"
	}
	if out.CooldownUntil > 0 && out.CooldownUntil <= time.Now().UnixMilli() && out.Status == "cooldown" {
		out.Status = "active"
	}
	return out
}
func (m *Manager) Run(ctx context.Context) {
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			m.RefreshAll(ctx)
			m.cleanup()
			timer.Reset(time.Duration(m.Settings().RefreshIntervalSeconds) * time.Second)
		}
	}
}
func (m *Manager) cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff := time.Now().Add(-time.Duration(m.settings.LogRetentionDays) * 24 * time.Hour).UnixMilli()
	if _, err := m.db.Exec("DELETE FROM logs WHERE created_at < ?", cutoff); err != nil {
		log.Printf("gateway log retention: %v", err)
	}
	_, _ = m.db.Exec("DELETE FROM logs WHERE id IN (SELECT id FROM logs ORDER BY created_at DESC LIMIT -1 OFFSET 100000)")
	now := time.Now()
	for k, s := range m.sessions {
		if now.After(s.Expires) {
			delete(m.sessions, k)
		}
	}
	for k, s := range m.affinity {
		if now.After(s.Expires) {
			delete(m.affinity, k)
		}
	}
	for k, a := range m.loginAttempts {
		if now.Sub(a.Since) > 15*time.Minute {
			delete(m.loginAttempts, k)
		}
	}
}
