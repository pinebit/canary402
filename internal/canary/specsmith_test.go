package canary

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAuditorSpecReviewAndRepairs(t *testing.T) {
	t.Parallel()
	var targetURL string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/openapi.json":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"openapi": "3.1.0",
				"paths": map[string]any{
					"/paid/{id}": map[string]any{
						"post": map[string]any{
							"summary": "Create a report",
							"requestBody": map[string]any{"content": map[string]any{"application/json": map[string]any{
								"schema":  map[string]any{"type": "object", "properties": map[string]any{"customer": map[string]any{"type": "string"}}},
								"example": map[string]any{"customer": "public-example"},
							}}},
							"responses": map[string]any{"200": map[string]any{"content": map[string]any{"application/json": map[string]any{
								"schema":  map[string]any{"type": "object", "properties": map[string]any{"report_id": map[string]any{"type": "string"}}},
								"example": map[string]any{"report_id": "example"},
							}}}},
						},
					},
				},
			})
		case "/.well-known/agent-registration.json":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"active":        true,
				"registrations": []map[string]any{{"agentId": 42}},
				"services":      []map[string]any{{"endpoint": targetURL + "/paid"}},
			})
		case "/skill.md":
			w.Header().Set("Content-Type", "text/markdown")
			fmt.Fprintln(w, "# Paid report service")
		case "/paid/42":
			challenge, err := json.Marshal(map[string]any{
				"x402Version": 2,
				"resource":    map[string]any{"url": targetURL + "/paid/42"},
				"accepts": []map[string]any{{
					"scheme": "exact", "network": "eip155:84532", "amount": "1000",
					"asset": baseSepoliaUSDC, "payTo": "0x1111111111111111111111111111111111111111",
				}},
				"extensions": map[string]any{"bazaar": map[string]any{"info": map[string]any{
					"input": map[string]any{"type": "http"}, "output": map[string]any{"type": "json"},
				}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			w.Header().Set("PAYMENT-REQUIRED", base64.StdEncoding.EncodeToString(challenge))
			w.WriteHeader(http.StatusPaymentRequired)
		default:
			http.NotFound(w, r)
		}
	}))
	defer target.Close()
	targetURL = target.URL

	auditor, store := testAuditor(t)
	report, err := auditor.Audit(context.Background(), AuditRequest{
		URL: target.URL + "/paid/42", Method: http.MethodPost,
		Body:       json.RawMessage(`{"customer":"DO-NOT-PUBLISH-THIS-VALUE","count":2}`),
		SpecReview: true, GenerateRepairs: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Verdict != "PROBE_PASS" || report.SpecReview == nil || report.SpecReview.Status != "READY" {
		t.Fatalf("unexpected spec verdict: %+v", report)
	}
	if !report.SpecReview.Operation.ConcreteRequestSchema || !report.SpecReview.Operation.ConcreteResponseSchema {
		t.Fatalf("operation schemas were not recognized: %+v", report.SpecReview.Operation)
	}
	if !report.SpecReview.Challenge.ResourceMatches || !report.SpecReview.Challenge.BazaarExtension {
		t.Fatalf("challenge metadata was not recognized: %+v", report.SpecReview.Challenge)
	}
	if report.SpecReview.Repairs == nil {
		t.Fatal("repair artifacts were not generated")
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "DO-NOT-PUBLISH-THIS-VALUE") {
		t.Fatal("generated report retained a caller example value")
	}
	stored, err := store.Get(report.ID)
	if err != nil || stored.SpecReview == nil {
		t.Fatalf("stored spec review missing: %v", err)
	}
}

func TestAuditorSpecReviewReportsRepairableGaps(t *testing.T) {
	t.Parallel()
	var targetURL string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/paid" {
			challenge, _ := json.Marshal(map[string]any{
				"x402Version": 2,
				"resource":    map[string]any{"url": strings.Replace(targetURL, "http://", "https://", 1) + "/wrong"},
				"accepts": []map[string]any{{
					"scheme": "exact", "network": "eip155:84532", "amount": "1000",
					"asset": baseSepoliaUSDC, "payTo": "0x1111111111111111111111111111111111111111",
				}},
			})
			w.Header().Set("PAYMENT-REQUIRED", base64.StdEncoding.EncodeToString(challenge))
			w.WriteHeader(http.StatusPaymentRequired)
			return
		}
		http.NotFound(w, r)
	}))
	defer target.Close()
	targetURL = target.URL

	auditor, _ := testAuditor(t)
	report, err := auditor.Audit(context.Background(), AuditRequest{URL: target.URL + "/paid", SpecReview: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.Verdict != "PROBE_PASS_WITH_WARNINGS" || report.SpecReview == nil || report.SpecReview.Status != "NEEDS_REPAIR" {
		t.Fatalf("unexpected warning report: %+v", report)
	}
	if report.Payment.Attempted {
		t.Fatal("spec review must not authorize a downstream payment")
	}
	if !hasFinding(report.SpecReview.Findings, "openapi_missing") || !hasFinding(report.SpecReview.Findings, "challenge_resource_mismatch") {
		t.Fatalf("expected findings missing: %+v", report.SpecReview.Findings)
	}
}

func TestGenerateRepairsRequiresSpecReview(t *testing.T) {
	t.Parallel()
	auditor, _ := testAuditor(t)
	_, err := auditor.Audit(context.Background(), AuditRequest{URL: "http://127.0.0.1", GenerateRepairs: true})
	if err == nil || !strings.Contains(err.Error(), "generate_repairs requires spec_review") {
		t.Fatalf("unexpected validation result: %v", err)
	}
}

func TestInferJSONSchemaProperties(t *testing.T) {
	t.Parallel()
	random := rand.New(rand.NewSource(402))
	for iteration := 0; iteration < 500; iteration++ {
		secretBytes := make([]byte, random.Intn(512))
		for index := range secretBytes {
			secretBytes[index] = byte(32 + random.Intn(95))
		}
		secret := string(secretBytes)
		count := random.Int63()
		enabled := random.Intn(2) == 1
		raw, err := json.Marshal(map[string]any{
			"secret_value": secret,
			"count":        count,
			"enabled":      enabled,
			"items":        []any{secret, count, enabled},
		})
		if err != nil {
			t.Fatal(err)
		}
		first := inferJSONSchema(raw)
		second := inferJSONSchema(raw)
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("schema inference was not deterministic at iteration %d", iteration)
		}
		if !schemaContainsOnlyStructuralValues(first) {
			t.Fatalf("schema retained non-structural values at iteration %d", iteration)
		}
	}
}

func FuzzAnalyzeOpenAPI(f *testing.F) {
	f.Add(`{"openapi":"3.1.0","paths":{}}`)
	f.Add(`{"openapi":"3.1.0","paths":{"/x":{"post":{"responses":{"200":{"content":{"application/json":{"schema":{"type":"object","properties":{"ok":{"type":"boolean"}}}}}}}}}}}`)
	f.Add(`{"openapi":"3.1.0","components":{"schemas":{"Loop":{"$ref":"#/components/schemas/Loop"}}},"paths":{"/x":{"post":{"requestBody":{"$ref":"#/components/schemas/Loop"}}}}}`)
	target, _ := url.Parse("https://example.com/x")
	f.Fuzz(func(t *testing.T, raw string) {
		document, err := decodeJSONObject([]byte(raw))
		if err != nil {
			return
		}
		first := &specInspection{result: SpecReviewResult{Requested: true}}
		second := &specInspection{result: SpecReviewResult{Requested: true}}
		first.analyzeOpenAPI(document, target, http.MethodPost)
		second.analyzeOpenAPI(document, target, http.MethodPost)
		if !reflect.DeepEqual(first.result, second.result) {
			t.Fatal("OpenAPI analysis is not deterministic")
		}
		if len(first.result.Findings) > maxSpecFindings {
			t.Fatalf("finding count exceeded bound: %d", len(first.result.Findings))
		}
	})
}

func FuzzInferJSONSchema(f *testing.F) {
	f.Add(`{"password":"never-copy-me","count":1}`)
	f.Add(`[1,"two",true,null]`)
	f.Add(`{"nested":{"items":[{"ok":true}]}}`)
	f.Fuzz(func(t *testing.T, raw string) {
		if !json.Valid([]byte(raw)) {
			return
		}
		first := inferJSONSchema([]byte(raw))
		second := inferJSONSchema([]byte(raw))
		if !reflect.DeepEqual(first, second) {
			t.Fatal("schema inference is not deterministic")
		}
		if first != nil && !schemaContainsOnlyStructuralValues(first) {
			t.Fatal("inferred schema retained non-structural values")
		}
	})
}

func testAuditor(t *testing.T) (*Auditor, *FileStore) {
	t.Helper()
	policy := PaymentPolicy{
		MaxAtomicAmount:  "20000",
		AllowedNetworks:  map[string]int64{"eip155:84532": 84532},
		AllowedAssets:    map[string]string{"eip155:84532": strings.ToLower(baseSepoliaUSDC)},
		AuthorizationTTL: time.Minute,
	}
	authorizer, err := NewPaymentAuthorizer(&fakeSigner{}, policy)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	client := NewSafeHTTPClient(TargetPolicy{AllowHTTP: true, AllowPrivateTargets: true, Timeout: 2 * time.Second, MaxResponseBytes: 64 << 10})
	auditor := NewAuditor(client, authorizer, nil, store, AuditConfig{MaxRequestBodyBytes: 64 << 10, MaxExpectationBytes: 2_000, SemanticInputBytes: 12_000})
	return auditor, store
}

func hasFinding(findings []SpecFinding, code string) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

func schemaContainsOnlyStructuralValues(schema map[string]any) bool {
	for key, value := range schema {
		switch key {
		case "type":
			typeName, ok := value.(string)
			if !ok || !containsString([]string{"object", "array", "string", "integer", "number", "boolean", "null"}, typeName) {
				return false
			}
		case "additionalProperties":
			if _, ok := value.(bool); !ok {
				return false
			}
		case "properties":
			properties, ok := value.(map[string]any)
			if !ok {
				return false
			}
			for _, property := range properties {
				nested, ok := property.(map[string]any)
				if !ok || !schemaContainsOnlyStructuralValues(nested) {
					return false
				}
			}
		case "items":
			nested, ok := value.(map[string]any)
			if !ok || !schemaContainsOnlyStructuralValues(nested) {
				return false
			}
		case "oneOf":
			variants, ok := value.([]any)
			if !ok {
				return false
			}
			for _, variant := range variants {
				nested, ok := variant.(map[string]any)
				if !ok || !schemaContainsOnlyStructuralValues(nested) {
					return false
				}
			}
		default:
			return false
		}
	}
	return true
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
