package canary

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

type SafeHTTPClient struct {
	client   *http.Client
	policy   TargetPolicy
	resolver *net.Resolver
}

func NewSafeHTTPClient(policy TargetPolicy) *SafeHTTPClient {
	resolver := net.DefaultResolver
	dialer := &net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   8 * time.Second,
		ResponseHeaderTimeout: policy.Timeout,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, fmt.Errorf("split target address: %w", err)
			}
			ips, err := resolveAndValidate(ctx, resolver, host, policy.AllowPrivateTargets)
			if err != nil {
				return nil, err
			}
			var errs []error
			for _, ip := range ips {
				conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if dialErr == nil {
					return conn, nil
				}
				errs = append(errs, dialErr)
			}
			return nil, fmt.Errorf("connect to validated target: %w", errors.Join(errs...))
		},
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   policy.Timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return &SafeHTTPClient{client: client, policy: policy, resolver: resolver}
}

func (s *SafeHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return s.client.Do(req)
}

func (s *SafeHTTPClient) ValidateURL(ctx context.Context, rawURL string) (*url.URL, error) {
	if len(rawURL) == 0 || len(rawURL) > 2_048 {
		return nil, fmt.Errorf("url must be between 1 and 2048 bytes")
	}
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid target url: %w", err)
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("target url must not contain credentials")
	}
	if parsed.Fragment != "" {
		return nil, fmt.Errorf("target url must not contain a fragment")
	}
	if parsed.Scheme != "https" && !(s.policy.AllowHTTP && parsed.Scheme == "http") {
		return nil, fmt.Errorf("target url must use https")
	}
	if parsed.Hostname() == "" {
		return nil, fmt.Errorf("target url must include a hostname")
	}
	if !isASCII(parsed.Hostname()) {
		return nil, fmt.Errorf("internationalized hostnames must be supplied in ASCII form")
	}
	port := parsed.Port()
	if parsed.Scheme == "https" && port != "" && port != "443" {
		return nil, fmt.Errorf("https target must use port 443")
	}
	if parsed.Scheme == "http" && port != "" && port != "80" && !s.policy.AllowPrivateTargets {
		return nil, fmt.Errorf("http target must use port 80")
	}
	if _, err := resolveAndValidate(ctx, s.resolver, parsed.Hostname(), s.policy.AllowPrivateTargets); err != nil {
		return nil, err
	}
	return parsed, nil
}

func resolveAndValidate(ctx context.Context, resolver *net.Resolver, host string, allowPrivate bool) ([]netip.Addr, error) {
	if ip, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		if !allowPrivate && !isSafePublicIP(ip) {
			return nil, fmt.Errorf("target resolves to a prohibited address")
		}
		return []netip.Addr{ip}, nil
	}
	addresses, err := resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("resolve target hostname: %w", err)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("target hostname has no addresses")
	}
	for _, ip := range addresses {
		if !allowPrivate && !isSafePublicIP(ip) {
			return nil, fmt.Errorf("target hostname resolves to a prohibited address")
		}
	}
	return addresses, nil
}

func isSafePublicIP(ip netip.Addr) bool {
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	prohibited := []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"),
		netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("192.0.0.0/24"),
		netip.MustParsePrefix("192.0.2.0/24"),
		netip.MustParsePrefix("198.18.0.0/15"),
		netip.MustParsePrefix("198.51.100.0/24"),
		netip.MustParsePrefix("203.0.113.0/24"),
		netip.MustParsePrefix("2001:db8::/32"),
	}
	for _, prefix := range prohibited {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

func redactedURL(parsed *url.URL) string {
	copy := *parsed
	copy.RawQuery = ""
	copy.ForceQuery = false
	copy.Fragment = ""
	copy.User = nil
	return copy.String()
}

func isASCII(value string) bool {
	for _, char := range value {
		if char > 127 {
			return false
		}
	}
	return true
}
