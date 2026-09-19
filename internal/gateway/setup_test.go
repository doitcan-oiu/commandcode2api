package gateway

import (
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConcurrentAdminSetupCreatesOnlyOneAdministrator(t *testing.T) {
	m := testManager(t)
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 8)
	for range 8 {
		go func() {
			<-start
			results <- request(t, m, "POST", "/api/admin/setup", map[string]string{"username": "admin", "password": "Test1234"}, nil)
		}()
	}
	close(start)
	succeeded, rejected := 0, 0
	for range 8 {
		w := <-results
		switch w.Code {
		case 200:
			succeeded++
		case 409:
			rejected++
		default:
			t.Errorf("unexpected setup response: %d %s", w.Code, w.Body.String())
		}
	}
	if succeeded != 1 || rejected != 7 {
		t.Fatalf("concurrent setup: %d succeeded, %d rejected", succeeded, rejected)
	}
}

func TestAdminPasswordBootstrapAndLegacySetupFile(t *testing.T) {
	dir := t.TempDir()
	legacyFile := filepath.Join(dir, "setup.token")
	if err := os.WriteFile(legacyFile, []byte("obsolete-fixture-token"), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := Open(Options{DataDir: dir, AdminPassword: "Test1234"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	if m.SetupRequired() {
		t.Fatal("environment bootstrap left setup open")
	}
	if _, err := os.Stat(legacyFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("obsolete setup token file retained")
	}
	if w := request(t, m, "POST", "/api/admin/login", map[string]string{"username": "admin", "password": "Test1234"}, nil); w.Code != 200 {
		t.Fatalf("environment bootstrap login failed: %d", w.Code)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(Options{DataDir: dir, AdminPassword: "Other123"})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if w := request(t, reopened, "POST", "/api/admin/login", map[string]string{"username": "admin", "password": "Test1234"}, nil); w.Code != 200 {
		t.Fatal("restart changed the existing administrator password")
	}
	w := request(t, reopened, "GET", "/api/admin/session", nil, nil)
	if !strings.Contains(w.Body.String(), `"setupRequired":false`) {
		t.Fatal("restart exposed administrator setup")
	}
}
