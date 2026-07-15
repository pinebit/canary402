package canary

import (
	"context"
	"net/netip"
	"testing"
	"time"
)

func TestSafePublicIP(t *testing.T) {
	t.Parallel()
	tests := []struct {
		address string
		safe    bool
	}{
		{"8.8.8.8", true},
		{"1.1.1.1", true},
		{"127.0.0.1", false},
		{"10.0.0.1", false},
		{"100.64.0.1", false},
		{"169.254.169.254", false},
		{"192.0.2.10", false},
		{"::1", false},
		{"2001:db8::1", false},
	}
	for _, test := range tests {
		t.Run(test.address, func(t *testing.T) {
			if got := isSafePublicIP(netip.MustParseAddr(test.address)); got != test.safe {
				t.Fatalf("isSafePublicIP(%s) = %v, want %v", test.address, got, test.safe)
			}
		})
	}
}

func TestValidateURLRejectsUnsafeTargets(t *testing.T) {
	t.Parallel()
	client := NewSafeHTTPClient(TargetPolicy{Timeout: time.Second})
	tests := []string{
		"http://example.com/audit",
		"https://user:pass@example.com/audit",
		"https://127.0.0.1/audit",
		"https://169.254.169.254/latest/meta-data",
		"https://example.com:8443/audit",
	}
	for _, target := range tests {
		if _, err := client.ValidateURL(context.Background(), target); err == nil {
			t.Errorf("expected %s to be rejected", target)
		}
	}
}
