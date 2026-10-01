package httpx

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func configureProxy(t *testing.T, proxy string) {
	t.Helper()
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
		t.Setenv(key, proxy)
	}
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")
	t.Setenv("REQUEST_METHOD", "")
}

func TestConfiguredPrivateProxyForHTTPAndHTTPS(t *testing.T) {
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "fixture response")
	}))
	defer origin.Close()
	var requests atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodConnect {
			fmt.Fprint(w, "fixture response")
			return
		}
		upstream, err := net.Dial("tcp", origin.Listener.Addr().String())
		if err != nil {
			t.Error("fixture upstream connection failed")
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer upstream.Close()
		conn, buffered, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error("fixture CONNECT hijack failed")
			return
		}
		defer conn.Close()
		fmt.Fprint(buffered, "HTTP/1.1 200 Connection Established\r\n\r\n")
		buffered.Flush()
		done := make(chan struct{})
		go func() {
			defer close(done)
			io.Copy(upstream, buffered)
			upstream.Close()
		}()
		io.Copy(conn, upstream)
		conn.Close()
		<-done
	}))
	defer proxy.Close()
	configureProxy(t, proxy.URL)
	client := New(2 * time.Second)
	defer client.CloseIdleConnections()
	transport := client.Transport.(*proxyTransport)
	// Certificate verification is irrelevant to this isolated tunnel fixture.
	transport.proxied.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	transport.lookupIP = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
	}
	for _, target := range []string{"http://8.8.8.8/fixture", "https://8.8.8.8/fixture", "https://public.example/fixture"} {
		resp, err := client.Get(target)
		if err != nil {
			t.Fatal("request through configured private proxy failed")
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || string(body) != "fixture response" {
			t.Fatal("unexpected proxy fixture response")
		}
	}
	if requests.Load() != 3 {
		t.Fatalf("proxy requests = %d, want 3", requests.Load())
	}
}

func TestProxyRejectsPrivateDestinationsAndDNSAnswers(t *testing.T) {
	var requests atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer proxy.Close()
	configureProxy(t, proxy.URL)
	client := New(time.Second)
	defer client.CloseIdleConnections()
	transport := client.Transport.(*proxyTransport)
	transport.lookupIP = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("10.0.0.1")}}, nil
	}
	for _, target := range []string{
		"http://10.0.0.1/", "https://192.168.1.1/", "http://169.254.169.254/",
		"https://[::1]/", "http://[fc00::1]/", "https://mixed.example/",
	} {
		resp, err := client.Get(target)
		if resp != nil {
			resp.Body.Close()
		}
		if err == nil {
			t.Fatal("non-public destination was accepted")
		}
	}
	if requests.Load() != 0 {
		t.Fatal("private destination was sent to the proxy")
	}
}

func TestProxyRedirectCannotReachPrivateDestination(t *testing.T) {
	var requests atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Redirect(w, r, "http://169.254.169.254/", http.StatusFound)
	}))
	defer proxy.Close()
	configureProxy(t, proxy.URL)
	client := New(time.Second)
	defer client.CloseIdleConnections()
	resp, err := client.Get("http://8.8.8.8/fixture")
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil || requests.Load() != 1 {
		t.Fatal("redirect must be rejected before contacting the private destination")
	}
}

func TestNoProxyUsesProtectedDirectConnection(t *testing.T) {
	configureProxy(t, "http://127.0.0.1:1")
	t.Setenv("NO_PROXY", "8.8.8.8")
	client := New(time.Second)
	defer client.CloseIdleConnections()
	sentinel := errors.New("isolated direct dial")
	transport := client.Transport.(*proxyTransport)
	transport.direct.DialContext = func(context.Context, string, string) (net.Conn, error) {
		return nil, sentinel
	}
	_, err := client.Get("http://8.8.8.8/fixture")
	if !errors.Is(err, sentinel) {
		t.Fatal("NO_PROXY did not select the direct transport")
	}
	// A new client keeps the real guarded dialer, including NO_PROXY traffic.
	t.Setenv("NO_PROXY", "*")
	direct := New(time.Second)
	defer direct.CloseIdleConnections()
	_, err = direct.Get("http://127.0.0.1:1/")
	if err == nil || !strings.Contains(err.Error(), "not a public address") {
		t.Fatal("direct private destination must still be blocked")
	}
}
