package sink

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var blockedSubnets []*net.IPNet

func init() {
	cidrs := []string{
		"10.0.0.0/8",     // RFC 1918
		"172.16.0.0/12",  // RFC 1918
		"192.168.0.0/16", // RFC 1918
		"127.0.0.0/8",    // Loopback
		"169.254.0.0/16", // Link-local / Cloud metadata (e.g. 169.254.169.254)
		"0.0.0.0/8",      // Current network / broadcast
		"::1/128",        // IPv6 Loopback
		"fc00::/7",       // IPv6 Unique Local Address
		"fe80::/10",      // IPv6 Link-Local
		"::/128",         // IPv6 Unspecified
	}
	for _, cidr := range cidrs {
		_, netBlock, err := net.ParseCIDR(cidr)
		if err == nil {
			blockedSubnets = append(blockedSubnets, netBlock)
		}
	}
}

// isBlockedIP checks if an IP belongs to private, loopback, link-local, or cloud metadata ranges.
func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	for _, block := range blockedSubnets {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

// ValidateWebhookURL validates that a webhook target URL uses http/https and does not resolve to private/loopback/cloud-metadata IPs.
func ValidateWebhookURL(rawURL string, allowPrivate bool) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("unsupported scheme %q: only http and https are allowed", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("URL missing host")
	}
	if !allowPrivate {
		if strings.EqualFold(host, "localhost") {
			return fmt.Errorf("webhook destination %q is blocked by anti-SSRF policy", host)
		}
		if ip := net.ParseIP(host); ip != nil {
			if isBlockedIP(ip) {
				return fmt.Errorf("webhook destination IP %s is blocked by anti-SSRF policy", ip.String())
			}
		} else {
			ips, err := net.LookupIP(host)
			if err != nil {
				return fmt.Errorf("resolving webhook host %q: %w", host, err)
			}
			if len(ips) == 0 {
				return fmt.Errorf("no IP addresses found for host %q", host)
			}
			for _, ip := range ips {
				if isBlockedIP(ip) {
					return fmt.Errorf("webhook destination %q resolves to blocked IP %s by anti-SSRF policy", host, ip.String())
				}
			}
		}
	}
	return nil
}

// createSafeWebhookClient creates an http.Client with a dialer and redirect handler that enforce anti-SSRF policy.
func createSafeWebhookClient(allowPrivate bool) *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				host, _, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				if !allowPrivate {
					if strings.EqualFold(host, "localhost") {
						return nil, fmt.Errorf("connecting to localhost is blocked by anti-SSRF policy")
					}
					ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
					if err != nil {
						return nil, fmt.Errorf("resolving host %q: %w", host, err)
					}
					for _, ip := range ips {
						if isBlockedIP(ip) {
							return nil, fmt.Errorf("connecting to %s (%s) is blocked by anti-SSRF policy", host, ip.String())
						}
					}
				}
				dialer := &net.Dialer{Timeout: 10 * time.Second}
				return dialer.DialContext(ctx, network, addr)
			},
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("stopped after 5 redirects")
			}
			if err := ValidateWebhookURL(req.URL.String(), allowPrivate); err != nil {
				return fmt.Errorf("webhook redirect blocked by anti-SSRF policy: %w", err)
			}
			return nil
		},
	}
}
