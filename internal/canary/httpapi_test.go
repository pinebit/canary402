package canary

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPHandlerRejectsUnknownFields(t *testing.T) {
	t.Parallel()
	policy := PaymentPolicy{MaxAtomicAmount: "10000", AllowedNetworks: map[string]int64{"eip155:84532": 84532}, AllowedAssets: map[string]string{"eip155:84532": strings.ToLower(baseSepoliaUSDC)}, AuthorizationTTL: time.Minute}
	authorizer, _ := NewPaymentAuthorizer(&fakeSigner{}, policy)
	store, _ := NewFileStore(t.TempDir())
	auditor := NewAuditor(NewSafeHTTPClient(TargetPolicy{Timeout: time.Second}), authorizer, nil, store, AuditConfig{MaxRequestBodyBytes: 64 << 10, MaxExpectationBytes: 2_000, SemanticInputBytes: 12_000})
	handler := NewHTTPHandler(auditor, store, HTTPHandlerConfig{MaxConcurrent: 1, Version: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})

	req := httptest.NewRequest(http.MethodPost, "/audit", bytes.NewBufferString(`{"url":"https://example.com","unexpected":true}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
}
