// Package httpx builds the HTTP clients the backend uses to talk to external
// services. Direct connections and destinations reached through a configured
// proxy reject private address ranges. An operator-configured proxy itself may
// be on a private network.
package httpx

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/http/httpproxy"

	"ytdm/backend/internal/apperr"
)

// DefaultTimeout is used when no timeout is configured.
const DefaultTimeout = 20 * time.Second

// maxRedirects bounds redirect chains.
const maxRedirects = 5

// New returns an HTTP client with sane timeouts and SSRF protection.
func New(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   controlAddress,
	}

	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 2 * time.Second,
		ResponseHeaderTimeout: timeout,
	}
	// Capture the operator's environment for this client, including NO_PROXY.
	// The public-address dialer must protect origin connections, not reject
	// the explicitly configured proxy's own private address.
	proxyFunc := httpproxy.FromEnvironment().ProxyFunc()
	proxy := func(req *http.Request) (*url.URL, error) { return proxyFunc(req.URL) }
	proxyDialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	proxied := transport.Clone()
	proxied.Proxy = proxy
	proxied.DialContext = proxyDialer.DialContext

	return &http.Client{
		Timeout: timeout,
		Transport: &proxyTransport{
			direct: transport, proxied: proxied, proxy: proxy,
			lookupIP: net.DefaultResolver.LookupIPAddr,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			if err := validateScheme(req.URL); err != nil {
				return err
			}
			return nil
		},
	}
}

type proxyTransport struct {
	direct, proxied *http.Transport
	proxy           func(*http.Request) (*url.URL, error)
	lookupIP        func(context.Context, string) ([]net.IPAddr, error)
}

func (t *proxyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	proxy, err := t.proxy(req)
	if err != nil {
		return nil, err
	}
	if proxy == nil {
		return t.direct.RoundTrip(req)
	}
	if err := validateScheme(req.URL); err != nil {
		return nil, err
	}
	// The proxy is trusted infrastructure, but provider-supplied destinations
	// are not. Check literals and DNS answers before handing them to the proxy,
	// including every redirect. The proxy must enforce its own egress policy
	// as well, since it resolves the origin independently.
	var addresses []net.IPAddr
	if ip := net.ParseIP(req.URL.Hostname()); ip != nil {
		addresses = []net.IPAddr{{IP: ip}}
	} else {
		addresses, err = t.lookupIP(req.Context(), req.URL.Hostname())
		if err != nil {
			return nil, fmt.Errorf("proxy destination could not be resolved: %w", err)
		}
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("proxy destination has no public address")
	}
	for _, address := range addresses {
		if !IsPublicIP(address.IP) {
			return nil, fmt.Errorf("proxy destination is not a public address")
		}
	}
	return t.proxied.RoundTrip(req)
}

func (t *proxyTransport) CloseIdleConnections() {
	t.direct.CloseIdleConnections()
	t.proxied.CloseIdleConnections()
}

// controlAddress rejects connections to addresses that are not routable on the
// public internet.
func controlAddress(network, address string, _ syscall.RawConn) error {
	if network != "tcp4" && network != "tcp6" && network != "tcp" {
		return fmt.Errorf("network %q is not allowed", network)
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("address %q could not be parsed: %w", address, err)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("address %q is not an IP address", host)
	}
	if !IsPublicIP(ip) {
		return fmt.Errorf("address %s is not a public address", ip)
	}
	return nil
}

// IsPublicIP reports whether an address may be contacted by the backend.
func IsPublicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return false
	}
	// Carrier grade NAT (100.64.0.0/10) and the IPv4 benchmarking range are
	// not covered by net.IP.IsPrivate.
	if v4 := ip.To4(); v4 != nil {
		switch {
		case v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127:
			return false
		case v4[0] == 198 && (v4[1] == 18 || v4[1] == 19):
			return false
		case v4[0] == 192 && v4[1] == 0 && v4[2] == 0:
			return false
		}
	}
	return true
}

// ValidateURL parses and checks an externally supplied URL.
func ValidateURL(raw string) (*url.URL, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, apperr.New(apperr.CodeInvalidRequest, "The URL must not be empty.")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInvalidRequest, "The URL could not be parsed.", err)
	}
	if err := validateScheme(parsed); err != nil {
		return nil, err
	}
	if parsed.Hostname() == "" {
		return nil, apperr.New(apperr.CodeInvalidRequest, "The URL has no host.")
	}
	if ip := net.ParseIP(parsed.Hostname()); ip != nil && !IsPublicIP(ip) {
		return nil, apperr.Newf(apperr.CodeInvalidRequest,
			"The URL points at the non-public address %s.", ip)
	}
	return parsed, nil
}

func validateScheme(u *url.URL) error {
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return nil
	default:
		return apperr.Newf(apperr.CodeInvalidRequest, "The URL scheme %q is not allowed.", u.Scheme)
	}
}

// HasHostSuffix reports whether the host of u ends in one of the given
// suffixes. It is used to keep media URLs on the platforms the backend
// actually supports.
func HasHostSuffix(u *url.URL, suffixes ...string) bool {
	host := strings.ToLower(u.Hostname())
	for _, suffix := range suffixes {
		suffix = strings.ToLower(strings.TrimPrefix(suffix, "."))
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}
