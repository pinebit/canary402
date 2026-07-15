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
	Pay              bool            `json:"pay,omitempty"`
	MaxPaymentAtomic string          `json:"max_payment_atomic,omitempty"`
	PaymentNetwork   string          `json:"payment_network,omitempty"`
	PaymentAsset     string          `json:"payment_asset,omitempty"`
}

type AuditReport struct {
	ID              string         `json:"id"`
	CreatedAt       time.Time      `json:"created_at"`
	Target          string         `json:"target"`
	Method          string         `json:"method"`
	Tier            string         `json:"tier"`
	Verdict         string         `json:"verdict"`
	Score           int            `json:"score"`
	CoveragePercent int            `json:"coverage_percent"`
	Summary         string         `json:"summary"`
	Checks          []Check        `json:"checks"`
	Probe           ProbeResult    `json:"probe"`
	Payment         PaymentResult  `json:"payment"`
	Semantic        SemanticResult `json:"semantic"`
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
