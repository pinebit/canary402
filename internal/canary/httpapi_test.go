package canary

import (
	"bytes"
	"encoding/json"
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

func TestHTTPHandlerAdvertisesSpecReview(t *testing.T) {
	t.Parallel()
	auditor, store := testAuditor(t)
	handler := NewHTTPHandler(auditor, store, HTTPHandlerConfig{MaxConcurrent: 1, Version: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	var document map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	components := document["components"].(map[string]any)
	paths := document["paths"].(map[string]any)
	if _, exists := paths["/"]; !exists {
		t.Fatal("OpenAPI is missing the free service landing operation")
	}
	schemas := components["schemas"].(map[string]any)
	auditRequest := schemas["AuditRequest"].(map[string]any)
	properties := auditRequest["properties"].(map[string]any)
	for _, field := range []string{"spec_review", "generate_repairs", "pay"} {
		if _, exists := properties[field]; !exists {
			t.Fatalf("OpenAPI is missing %s", field)
		}
	}
}

func TestHTTPHandlerServesLandingPage(t *testing.T) {
	t.Parallel()
	auditor, store := testAuditor(t)
	handler := NewHTTPHandler(auditor, store, HTTPHandlerConfig{MaxConcurrent: 1, Version: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if contentType := resp.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("content type=%q", contentType)
	}
	if !strings.Contains(resp.Body.String(), "Service-contract inspector") {
		t.Fatalf("unexpected landing page: %s", resp.Body.String())
	}
}
