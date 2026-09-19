package webui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaticRoutesAndSPA(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html><title>Gateway</title>"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("export default {}"), 0600); err != nil {
		t.Fatal(err)
	}
	h := Handler(dir)
	for _, tc := range []struct {
		method, path string
		status       int
		contains     string
	}{{"GET", "/", 200, "Gateway"}, {"GET", "/accounts", 200, "Gateway"}, {"GET", "/assets/app.js", 200, "export default"}, {"GET", "/assets/missing.js", 404, ""}, {"GET", "/missing.ico", 404, ""}, {"HEAD", "/settings", 200, ""}, {"POST", "/settings", 405, ""}} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.contains) {
			t.Errorf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body)
		}
		if tc.method == "HEAD" && w.Body.Len() != 0 {
			t.Fatal("HEAD returned a body")
		}
		if tc.path == "/assets/app.js" && !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
			t.Fatal("hashed assets missing immutable cache")
		}
		if tc.method == "GET" && w.Header().Get("Content-Security-Policy") == "" {
			t.Fatal("missing content security policy")
		}
	}
	missing := httptest.NewRecorder()
	Handler(t.TempDir()).ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/", nil))
	if missing.Code != 503 {
		t.Fatal("missing build must not look healthy")
	}
}
