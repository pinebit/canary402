package canary

import (
	"context"
	"encoding/binary"
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
		{"::ffff:100.64.0.1", false},
		{"::ffff:198.18.0.1", false},
		{"::ffff:203.0.113.10", false},
		{"64:ff9b::a9fe:a9fe", false},
		{"64:ff9b:1::1", false},
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
		"https://[::ffff:100.64.0.1]/audit",
		"https://[::ffff:198.18.0.1]/audit",
		"https://[64:ff9b::a9fe:a9fe]/latest/meta-data",
		"https://example.com:8443/audit",
	}
	for _, target := range tests {
		if _, err := client.ValidateURL(context.Background(), target); err == nil {
			t.Errorf("expected %s to be rejected", target)
		}
	}
}

func FuzzMappedIPv4HasSameSafetyClassification(f *testing.F) {
	for _, seed := range []uint32{0, 0x08080808, 0x64400001, 0x7f000001, 0xa9fea9fe, 0xc6120001, 0xcb00710a, ^uint32(0)} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw uint32) {
		var v4Bytes [4]byte
		binary.BigEndian.PutUint32(v4Bytes[:], raw)
		v4 := netip.AddrFrom4(v4Bytes)

		var mappedBytes [16]byte
		mappedBytes[10] = 0xff
		mappedBytes[11] = 0xff
		copy(mappedBytes[12:], v4Bytes[:])
		mapped := netip.AddrFrom16(mappedBytes)

		if got, want := isSafePublicIP(mapped), isSafePublicIP(v4); got != want {
			t.Fatalf("mapped classification for %s = %v, want %v", v4, got, want)
		}
	})
}
