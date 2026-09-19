package gateway

import (
	"crypto/pbkdf2"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const sessionCookie = "ccg_admin"

func hashPassword(password string) string {
	salt := []byte(randomToken(24))
	key, err := pbkdf2.Key(sha256.New, password, salt, 600000, 32)
	if err != nil {
		panic(err)
	}
	return base64.RawStdEncoding.EncodeToString(salt) + ":" + base64.RawStdEncoding.EncodeToString(key)
}
func checkPassword(password, encoded string) bool {
	parts := strings.Split(encoded, ":")
	if len(parts) != 2 {
		return false
	}
	salt, e1 := base64.RawStdEncoding.DecodeString(parts[0])
	expected, e2 := base64.RawStdEncoding.DecodeString(parts[1])
	if e1 != nil || e2 != nil {
		return false
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, 600000, 32)
	return err == nil && subtle.ConstantTimeCompare(key, expected) == 1
}
func validPassword(s string) bool { return len(s) >= 8 && len(s) <= 1024 }
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, err error) {
	var e *Error
	if !errors.As(err, &e) {
		e = apiError(500, "Internal storage error")
	}
	if e.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconvI(e.RetryAfter))
	}
	writeJSON(w, e.Status, map[string]any{"error": map[string]string{"message": e.Message}})
}
func readJSON(w http.ResponseWriter, r *http.Request, v any) error {
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		return apiError(415, "Content-Type must be application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return apiError(400, "Invalid JSON body or unsupported field")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return apiError(400, "Request must contain exactly one JSON object")
	}
	return nil
}
func sameOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && strings.EqualFold(u.Host, r.Host) && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == ""
}
func (m *Manager) authenticatedLocked(r *http.Request) bool {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	s, ok := m.sessions[digest(c.Value)]
	if ok && time.Now().Before(s.Expires) {
		return true
	}
	return false
}
func (m *Manager) setSessionLocked(w http.ResponseWriter, r *http.Request) {
	token := randomToken(32)
	if len(m.sessions) >= 100 {
		for k := range m.sessions {
			delete(m.sessions, k)
			break
		}
	}
	m.sessions[digest(token)] = session{Expires: time.Now().Add(24 * time.Hour)}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/api/admin", MaxAge: 86400, HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"})
}
func clearSession(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/api/admin", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"})
}
func (m *Manager) checkLoginRateLocked(r *http.Request) bool {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	now := time.Now()
	a := m.loginAttempts[ip]
	if now.Sub(a.Since) > 15*time.Minute {
		a = attempt{Since: now}
	}
	if a.Count >= 10 {
		return false
	}
	if len(m.loginAttempts) >= 10000 {
		for k, v := range m.loginAttempts {
			if now.Sub(v.Since) > 15*time.Minute {
				delete(m.loginAttempts, k)
			}
		}
		if len(m.loginAttempts) >= 10000 {
			return false
		}
	}
	a.Count++
	m.loginAttempts[ip] = a
	return true
}
func (m *Manager) handleSession(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	authenticated := m.authenticatedLocked(r)
	username := ""
	if authenticated {
		username = m.admin.Username
	}
	writeJSON(w, 200, map[string]any{"authenticated": authenticated, "setupRequired": m.admin.Username == "", "username": username})
}
func (m *Manager) handleSetup(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, err)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.admin.Username != "" {
		writeError(w, apiError(409, "Administrator is already configured"))
		return
	}
	if !m.checkLoginRateLocked(r) {
		writeError(w, &Error{Status: 429, Message: "Too many setup attempts; retry in 15 minutes", RetryAfter: 900})
		return
	}
	input.Username = strings.TrimSpace(input.Username)
	if len(input.Username) < 1 || len(input.Username) > 64 || !validPassword(input.Password) {
		writeError(w, apiError(400, "Username must be 1–64 characters and password 8–1024 characters"))
		return
	}
	a := admin{Username: input.Username, PasswordHash: hashPassword(input.Password)}
	if err := m.saveMeta("admin", a); err != nil {
		writeError(w, err)
		return
	}
	m.admin = a
	m.setSessionLocked(w, r)
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (m *Manager) handleLogin(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, err)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.checkLoginRateLocked(r) {
		writeError(w, &Error{Status: 429, Message: "Too many login attempts; retry in 15 minutes", RetryAfter: 900})
		return
	}
	if len(input.Password) > 1024 || !checkPassword(input.Password, m.admin.PasswordHash) || input.Username != m.admin.Username {
		writeError(w, apiError(401, "Invalid username or password"))
		return
	}
	m.setSessionLocked(w, r)
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (m *Manager) handleLogout(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	if c, err := r.Cookie(sessionCookie); err == nil {
		delete(m.sessions, digest(c.Value))
	}
	m.mu.Unlock()
	clearSession(w, r)
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (m *Manager) handlePassword(w http.ResponseWriter, r *http.Request) {
	var input struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, err)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !validPassword(input.NewPassword) {
		writeError(w, apiError(400, "Password must contain 8–1024 characters"))
		return
	}
	if len(input.CurrentPassword) > 1024 || !checkPassword(input.CurrentPassword, m.admin.PasswordHash) {
		writeError(w, apiError(401, "Current password is incorrect"))
		return
	}
	a := admin{Username: m.admin.Username, PasswordHash: hashPassword(input.NewPassword)}
	if err := m.saveMeta("admin", a); err != nil {
		writeError(w, err)
		return
	}
	m.admin = a
	m.sessions = map[string]session{}
	m.setSessionLocked(w, r)
	writeJSON(w, 200, map[string]bool{"ok": true})
}
