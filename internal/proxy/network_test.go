package proxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"commandcode2api/internal/config"
)

type networkObservation struct {
	method, host, path string
	headers            http.Header
	tls                bool
}

// The test proxy only tunnels to the specified local test server. The allowlist
// makes it impossible for a regression to make this test connect externally.
func localConnectProxy(t *testing.T, targetAddress string) (*httptest.Server, <-chan networkObservation) {
	t.Helper()
	observed := make(chan networkObservation, 16)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed <- networkObservation{method: r.Method, host: r.Host, path: r.URL.Path, headers: r.Header.Clone()}
		if r.Method != http.MethodConnect || r.Host != targetAddress {
			http.Error(w, "only the local CONNECT target is allowed", http.StatusBadGateway)
			return
		}
		upstream, err := net.DialTimeout("tcp", targetAddress, 2*time.Second)
		if err != nil {
			http.Error(w, "local target unavailable", http.StatusBadGateway)
			return
		}
		downstream, buffered, err := w.(http.Hijacker).Hijack()
		if err != nil {
			upstream.Close()
			return
		}
		defer downstream.Close()
		defer upstream.Close()
		// Bound both halves even if the client fails to close a test connection.
		_ = downstream.SetDeadline(time.Now().Add(5 * time.Second))
		_ = upstream.SetDeadline(time.Now().Add(5 * time.Second))
		_, _ = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		if buffered.Flush() != nil {
			return
		}
		copied := make(chan struct{})
		go func() {
			_, _ = io.Copy(upstream, buffered)
			upstream.Close()
			close(copied)
		}()
		_, _ = io.Copy(downstream, upstream)
		downstream.Close()
		upstream.Close()
		<-copied
	}))
	t.Cleanup(proxy.Close)
	return proxy, observed
}

func networkObserved(t *testing.T, observations <-chan networkObservation) networkObservation {
	t.Helper()
	select {
	case observed := <-observations:
		return observed
	case <-time.After(3 * time.Second):
		t.Fatal("local server did not receive the expected request")
		return networkObservation{}
	}
}

func networkTrustRoots(t *testing.T, transport *http.Transport, roots *x509.CertPool, serverName string) {
	t.Helper()
	tlsConfig := transport.TLSClientConfig
	if tlsConfig == nil {
		tlsConfig = &tls.Config{}
	} else {
		tlsConfig = tlsConfig.Clone()
	}
	if tlsConfig.InsecureSkipVerify {
		t.Fatal("upstream transport must not disable target TLS verification")
	}
	// Preserve the transport's verification policy; only add the local CA and
	// optional hostname needed by the test instead of bypassing verification.
	tlsConfig.RootCAs, tlsConfig.ServerName = roots, serverName
	transport.TLSClientConfig = tlsConfig
}

func TestNetworkHTTPSConnectAuthenticationAndRegistryIsolation(t *testing.T) {
	targetObserved := make(chan networkObservation, 8)
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetObserved <- networkObservation{method: r.Method, host: r.Host, path: r.URL.Path, headers: r.Header.Clone(), tls: r.TLS != nil}
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(target.Close)
	targetURL, _ := url.Parse(target.URL)
	connectProxy, proxyObserved := localConnectProxy(t, targetURL.Host)
	proxyURL, _ := url.Parse(connectProxy.URL)
	proxyURL.User = url.UserPassword("proxy-user", "proxy-password")

	// These settings must not override the explicit CC proxy or affect npm.
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("NO_PROXY", "*")
	cfg := config.Default()
	cfg.APIBase, cfg.UpstreamProxy = target.URL, proxyURL.String()
	p, err := New(cfg, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	transport := p.client.Transport.(*http.Transport)
	registryTransport := p.registryClient.Transport.(*http.Transport)
	if transport.Proxy == nil || registryTransport.Proxy != nil {
		t.Fatal("CC must use its configured proxy while registry transport must be direct")
	}
	if transport == registryTransport {
		t.Fatal("CC and registry must not share one mutable transport")
	}
	roots := x509.NewCertPool()
	roots.AddCert(target.Certificate())
	networkTrustRoots(t, transport, roots, "")
	networkTrustRoots(t, registryTransport, roots, "")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	response, err := p.request(ctx, http.MethodPost, "/alpha/generate", M{"message": "hello"}, p.authHeaders("user_target-key"))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT request failed with status %d", response.StatusCode)
	}
	connect := networkObserved(t, proxyObserved)
	if connect.method != http.MethodConnect || connect.host != targetURL.Host {
		t.Fatal("HTTPS traffic did not use a CONNECT tunnel to the target")
	}
	wantProxyAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("proxy-user:proxy-password"))
	if connect.headers.Get("Proxy-Authorization") != wantProxyAuth {
		t.Fatal("proxy credentials were not sent in CONNECT Proxy-Authorization")
	}
	if connect.headers.Get("Authorization") != "" {
		t.Fatal("CC Authorization leaked to the HTTP proxy")
	}
	received := networkObserved(t, targetObserved)
	if !received.tls || received.path != "/alpha/generate" || received.headers.Get("Authorization") != "Bearer user_target-key" {
		t.Fatal("the TLS target did not receive the authorized generation request")
	}
	if received.headers.Get("Proxy-Authorization") != "" {
		t.Fatal("proxy credentials leaked inside the tunnel to the target")
	}

	// Exercise the registry transport against a local HTTPS URL. No external npm
	// request is made, and the ordinary registry request has no CC credentials.
	registryRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, target.URL+"/command-code/latest", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err = p.registryClient.Do(registryRequest)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	received = networkObserved(t, targetObserved)
	if !received.tls || received.path != "/command-code/latest" {
		t.Fatal("registry transport failed to reach its direct HTTPS target")
	}
	if received.headers.Get("Authorization") != "" || received.headers.Get("Proxy-Authorization") != "" {
		t.Fatal("CC or proxy authorization leaked to the registry request")
	}
	select {
	case <-proxyObserved:
		t.Fatal("registry traffic incorrectly inherited the CC proxy")
	default:
	}
}

func TestNetworkCONNECTRejectsUntrustedAndWrongHostnameCertificates(t *testing.T) {
	target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("request reached the handler despite invalid target TLS verification")
		w.WriteHeader(http.StatusOK)
	}))
	target.Config.ErrorLog = log.New(io.Discard, "", 0)
	target.StartTLS()
	t.Cleanup(target.Close)
	targetURL, _ := url.Parse(target.URL)
	connectProxy, observed := localConnectProxy(t, targetURL.Host)
	for _, test := range []struct {
		name       string
		trustRoot  bool
		serverName string
	}{
		{name: "untrusted issuer"},
		{name: "wrong target hostname", trustRoot: true, serverName: "wrong-target.invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.APIBase, cfg.UpstreamProxy = target.URL, connectProxy.URL
			p, err := New(cfg, log.New(io.Discard, "", 0))
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			roots := x509.NewCertPool()
			if test.trustRoot {
				roots.AddCert(target.Certificate())
			}
			networkTrustRoots(t, p.client.Transport.(*http.Transport), roots, test.serverName)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			response, err := p.request(ctx, http.MethodGet, "/verify", nil, p.authHeaders("user_test"))
			if response != nil {
				response.Body.Close()
			}
			if err == nil {
				t.Fatal("invalid target TLS certificate was accepted")
			}
			if test.trustRoot {
				var certificateError x509.HostnameError
				if !errors.As(err, &certificateError) {
					t.Fatalf("expected a hostname verification error, got %v", err)
				}
			} else {
				var certificateError x509.UnknownAuthorityError
				if !errors.As(err, &certificateError) {
					t.Fatalf("expected an untrusted issuer error, got %v", err)
				}
			}
			if connect := networkObserved(t, observed); connect.method != http.MethodConnect {
				t.Fatal("TLS validation did not run through the proxy tunnel")
			}
		})
	}
}

func TestNetworkInvalidProxyURLDoesNotExposeCredentials(t *testing.T) {
	for _, proxyURL := range []string{
		"socks5://private-user:private-password@127.0.0.1:8080",
		"https://private-user:private-password@127.0.0.1:8080",
		"http://private-user:private-password@/missing-host",
		"http://private-user:private-password@127.0.0.1:invalid",
		"http://private-user:private-password%zz@127.0.0.1:8080",
	} {
		cfg := config.Default()
		cfg.APIBase, cfg.UpstreamProxy = "http://127.0.0.1:1", proxyURL
		p, err := New(cfg, log.New(io.Discard, "", 0))
		if err == nil {
			p.Close()
			t.Fatal("invalid proxy URL was accepted")
		}
		for _, secret := range []string{"private-user", "private-password", proxyURL} {
			if strings.Contains(err.Error(), secret) {
				t.Fatal("proxy validation error exposed credentials")
			}
		}
		if !strings.Contains(err.Error(), "upstreamProxy") {
			t.Fatal("proxy validation error did not identify the configuration field")
		}
	}
}
