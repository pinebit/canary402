package canary

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeSigner struct {
	address   string
	signature string
	typedData map[string]any
}

func (f *fakeSigner) Address(context.Context) (string, error) {
	return f.address, nil
}

func (f *fakeSigner) SignTypedData(_ context.Context, _ string, typedData map[string]any) (string, error) {
	f.typedData = typedData
	return f.signature, nil
}

type fakeEvaluator struct{}

func (fakeEvaluator) Evaluate(_ context.Context, expectation, body string, status int, contentType string) (SemanticResult, error) {
	return SemanticResult{
		Attempted: true,
		Passed:    strings.Contains(body, "22") && strings.Contains(expectation, "temperature"),
		Score:     96,
		Reason:    "The response contains a numeric temperature.",
		Model:     "test-model",
	}, nil
}

func TestAuditorPaidEndToEnd(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("PAYMENT-SIGNATURE") == "" {
			challenge, err := json.Marshal(map[string]any{
				"x402Version": 2,
				"accepts": []map[string]any{{
					"scheme": "exact", "network": "eip155:84532", "amount": "1000",
					"asset": baseSepoliaUSDC, "payTo": "0x1111111111111111111111111111111111111111",
					"maxTimeoutSeconds": 300,
					"extra":             map[string]any{"assetTransferMethod": "eip3009"},
				}},
				"extensions": map[string]any{"bazaar": map[string]any{"info": map[string]any{"discoverable": true}, "schema": map[string]any{"ignored": true}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			w.Header().Set("PAYMENT-REQUIRED", base64.StdEncoding.EncodeToString(challenge))
			w.WriteHeader(http.StatusPaymentRequired)
			return
		}
		decoded, err := base64.StdEncoding.DecodeString(r.Header.Get("PAYMENT-SIGNATURE"))
		if err != nil {
			t.Errorf("decode payment header: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var envelope map[string]any
		if err := json.Unmarshal(decoded, &envelope); err != nil {
			t.Errorf("decode payment envelope: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if envelope["x402Version"] != float64(2) {
			t.Errorf("unexpected payment envelope: %#v", envelope)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("PAYMENT-RESPONSE", "settled-test-transaction")
		json.NewEncoder(w).Encode(map[string]any{"city": "Istanbul", "temperature": 22})
	}))
	defer target.Close()

	policy := PaymentPolicy{
		MaxAtomicAmount:  "10000",
		AllowedNetworks:  map[string]int64{"eip155:84532": 84532},
		AllowedAssets:    map[string]string{"eip155:84532": strings.ToLower(baseSepoliaUSDC)},
		AuthorizationTTL: 30 * time.Minute,
	}
	signer := &fakeSigner{
		address:   "0x2222222222222222222222222222222222222222",
		signature: "0x" + strings.Repeat("ab", 65),
	}
	authorizer, err := NewPaymentAuthorizer(signer, policy)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	targetClient := NewSafeHTTPClient(TargetPolicy{
		AllowHTTP: true, AllowPrivateTargets: true, Timeout: 2 * time.Second, MaxResponseBytes: 64 << 10,
	})
	auditor := NewAuditor(targetClient, authorizer, fakeEvaluator{}, store, AuditConfig{
		MaxRequestBodyBytes: 64 << 10, MaxExpectationBytes: 2_000, SemanticInputBytes: 12_000,
	})
	report, err := auditor.Audit(context.Background(), AuditRequest{
		URL: target.URL, Method: http.MethodGet,
		Expectation: "The response contains an Istanbul temperature.",
		Pay:         true, MaxPaymentAtomic: "1000", PaymentNetwork: "base-sepolia",
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("target calls = %d, want 2", calls.Load())
	}
	if report.Verdict != "PASS" || report.Tier != "verified" {
		t.Fatalf("unexpected verdict: tier=%s verdict=%s report=%+v", report.Tier, report.Verdict, report)
	}
	if report.Score != 99 || report.CoveragePercent != 100 {
		t.Fatalf("score=%d coverage=%d, want 99/100", report.Score, report.CoveragePercent)
	}
	if !report.Semantic.Passed || report.Payment.SettlementEvidence == "" {
		t.Fatalf("semantic or settlement evidence missing: %+v", report)
	}
	if report.Probe.ChallengeTransport != "PAYMENT-REQUIRED" || report.Payment.AuthorizationHeader != "PAYMENT-SIGNATURE" {
		t.Fatalf("current x402 headers were not used: %+v", report)
	}
	stored, err := store.Get(report.ID)
	if err != nil || stored.ID != report.ID {
		t.Fatalf("stored report missing: %+v %v", stored, err)
	}
	if signer.typedData == nil {
		t.Fatal("typed data was not sent to the signer")
	}
}

func TestAuditorProbeOnly(t *testing.T) {
	t.Parallel()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		json.NewEncoder(w).Encode(map[string]any{
			"x402Version": 2,
			"accepts": []map[string]any{{
				"scheme": "exact", "network": "eip155:84532", "amount": "1000",
				"asset": baseSepoliaUSDC, "payTo": "0x1111111111111111111111111111111111111111",
			}},
		})
	}))
	defer target.Close()

	policy := PaymentPolicy{MaxAtomicAmount: "10000", AllowedNetworks: map[string]int64{"eip155:84532": 84532}, AllowedAssets: map[string]string{"eip155:84532": strings.ToLower(baseSepoliaUSDC)}, AuthorizationTTL: time.Minute}
	authorizer, _ := NewPaymentAuthorizer(&fakeSigner{}, policy)
	store, _ := NewFileStore(t.TempDir())
	auditor := NewAuditor(
		NewSafeHTTPClient(TargetPolicy{AllowHTTP: true, AllowPrivateTargets: true, Timeout: time.Second, MaxResponseBytes: 64 << 10}),
		authorizer, nil, store,
		AuditConfig{MaxRequestBodyBytes: 64 << 10, MaxExpectationBytes: 2_000, SemanticInputBytes: 12_000},
	)
	report, err := auditor.Audit(context.Background(), AuditRequest{URL: target.URL})
	if err != nil {
		t.Fatal(err)
	}
	if report.Verdict != "PROBE_PASS" || report.CoveragePercent != 40 || report.Payment.Attempted {
		t.Fatalf("unexpected probe report: %+v", report)
	}
}
