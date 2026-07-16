package canary

import (
	"encoding/json"
	"time"
)

type AuditRequest struct {
	URL              string          `json:"url"`
	Method           string          `json:"method,omitempty"`
	Body             json.RawMessage `json:"body,omitempty"`
	ContentType      string          `json:"content_type,omitempty"`
	Expectation      string          `json:"expectation,omitempty"`
	ExpectedStatus   int             `json:"expected_status,omitempty"`
	SpecReview       bool            `json:"spec_review,omitempty"`
	GenerateRepairs  bool            `json:"generate_repairs,omitempty"`
	Pay              bool            `json:"pay,omitempty"`
	MaxPaymentAtomic string          `json:"max_payment_atomic,omitempty"`
	PaymentNetwork   string          `json:"payment_network,omitempty"`
	PaymentAsset     string          `json:"payment_asset,omitempty"`
}

type AuditReport struct {
	ID              string            `json:"id"`
	CreatedAt       time.Time         `json:"created_at"`
	Target          string            `json:"target"`
	Method          string            `json:"method"`
	Tier            string            `json:"tier"`
	Verdict         string            `json:"verdict"`
	Score           int               `json:"score"`
	CoveragePercent int               `json:"coverage_percent"`
	Summary         string            `json:"summary"`
	Checks          []Check           `json:"checks"`
	Probe           ProbeResult       `json:"probe"`
	Payment         PaymentResult     `json:"payment"`
	Semantic        SemanticResult    `json:"semantic"`
	SpecReview      *SpecReviewResult `json:"spec_review,omitempty"`
}

type SpecReviewResult struct {
	Requested bool                `json:"requested"`
	Status    string              `json:"status"`
	Documents []SpecDocument      `json:"documents"`
	Operation SpecOperation       `json:"operation"`
	Challenge SpecChallengeReview `json:"challenge"`
	Findings  []SpecFinding       `json:"findings"`
	Repairs   *SpecRepairBundle   `json:"repairs,omitempty"`
}

type SpecDocument struct {
	Kind              string `json:"kind"`
	URL               string `json:"url"`
	StatusCode        int    `json:"status_code,omitempty"`
	ContentType       string `json:"content_type,omitempty"`
	BodyBytes         int    `json:"body_bytes,omitempty"`
	BodySHA256        string `json:"body_sha256,omitempty"`
	Available         bool   `json:"available"`
	Valid             bool   `json:"valid"`
	ResponseTruncated bool   `json:"response_truncated,omitempty"`
	Error             string `json:"error,omitempty"`
}

type SpecOperation struct {
	Found                  bool   `json:"found"`
	OpenAPIPath            string `json:"openapi_path,omitempty"`
	Method                 string `json:"method,omitempty"`
	Summary                string `json:"summary,omitempty"`
	RequestSchema          bool   `json:"request_schema"`
	ConcreteRequestSchema  bool   `json:"concrete_request_schema"`
	ResponseSchema         bool   `json:"response_schema"`
	ConcreteResponseSchema bool   `json:"concrete_response_schema"`
	RequestExample         bool   `json:"request_example"`
	ResponseExample        bool   `json:"response_example"`
}

type SpecChallengeReview struct {
	ResourceURL        string `json:"resource_url,omitempty"`
	ResourceMatches    bool   `json:"resource_matches"`
	BazaarExtension    bool   `json:"bazaar_extension"`
	BazaarInputSchema  bool   `json:"bazaar_input_schema"`
	BazaarOutputSchema bool   `json:"bazaar_output_schema"`
}

type SpecFinding struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Repair   string `json:"repair,omitempty"`
}

type SpecRepairBundle struct {
	OpenAPIPatch      map[string]any      `json:"openapi_patch"`
	BazaarDeclaration map[string]any      `json:"bazaar_declaration"`
	RequestTemplate   SpecRequestTemplate `json:"request_template"`
	ReviewRequired    []string            `json:"review_required"`
}

type SpecRequestTemplate struct {
	URL         string `json:"url"`
	Method      string `json:"method"`
	ContentType string `json:"content_type,omitempty"`
	BodyFile    string `json:"body_file,omitempty"`
}

type Check struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Weight   int    `json:"weight"`
	Points   int    `json:"points"`
	Evidence string `json:"evidence"`
}

type ProbeResult struct {
	StatusCode         int                    `json:"status_code,omitempty"`
	LatencyMS          int64                  `json:"latency_ms,omitempty"`
	ContentType        string                 `json:"content_type,omitempty"`
	BodyBytes          int                    `json:"body_bytes,omitempty"`
	BodySHA256         string                 `json:"body_sha256,omitempty"`
	X402Version        int                    `json:"x402_version,omitempty"`
	ChallengeTransport string                 `json:"challenge_transport,omitempty"`
	PaymentOptions     []PaymentOptionSummary `json:"payment_options,omitempty"`
	ResponseTruncated  bool                   `json:"response_truncated,omitempty"`
}

type PaymentOptionSummary struct {
	Scheme         string `json:"scheme"`
	Network        string `json:"network"`
	Amount         string `json:"amount"`
	Asset          string `json:"asset"`
	PayTo          string `json:"pay_to"`
	TransferMethod string `json:"transfer_method"`
}

type PaymentResult struct {
	Requested           bool                  `json:"requested"`
	Attempted           bool                  `json:"attempted"`
	Selected            *PaymentOptionSummary `json:"selected,omitempty"`
	Payer               string                `json:"payer,omitempty"`
	AuthorizationHeader string                `json:"authorization_header,omitempty"`
	StatusCode          int                   `json:"status_code,omitempty"`
	LatencyMS           int64                 `json:"latency_ms,omitempty"`
	ContentType         string                `json:"content_type,omitempty"`
	BodyBytes           int                   `json:"body_bytes,omitempty"`
	BodySHA256          string                `json:"body_sha256,omitempty"`
	ResponseTruncated   bool                  `json:"response_truncated,omitempty"`
	SettlementEvidence  string                `json:"settlement_evidence,omitempty"`
	Error               string                `json:"error,omitempty"`
}

type SemanticResult struct {
	Attempted bool   `json:"attempted"`
	Passed    bool   `json:"passed,omitempty"`
	Score     int    `json:"score,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Model     string `json:"model,omitempty"`
	Error     string `json:"error,omitempty"`
}

const (
	checkPassed  = "pass"
	checkWarning = "warn"
	checkFailed  = "fail"
	checkSkipped = "skip"
)
