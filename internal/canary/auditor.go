package canary

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

type reportStore interface {
	Save(AuditReport) error
}

type Auditor struct {
	target     *SafeHTTPClient
	authorizer *PaymentAuthorizer
	evaluator  SemanticEvaluator
	store      reportStore
	config     AuditConfig
}

func NewAuditor(target *SafeHTTPClient, authorizer *PaymentAuthorizer, evaluator SemanticEvaluator, store reportStore, config AuditConfig) *Auditor {
	return &Auditor{target: target, authorizer: authorizer, evaluator: evaluator, store: store, config: config}
}

type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

func (a *Auditor) Audit(ctx context.Context, request AuditRequest) (AuditReport, error) {
	parsedURL, method, err := a.validateRequest(ctx, request)
	if err != nil {
		return AuditReport{}, err
	}
	id, err := newReportID()
	if err != nil {
		return AuditReport{}, fmt.Errorf("generate report id: %w", err)
	}
	report := AuditReport{
		ID:        id,
		CreatedAt: time.Now().UTC(),
		Target:    redactedURL(parsedURL),
		Method:    method,
		Tier:      "probe",
		Payment:   PaymentResult{Requested: request.Pay},
	}
	var specInspection *specInspection
	if request.SpecReview {
		specInspection = a.inspectSpecifications(ctx, parsedURL, method, request.Body, request.GenerateRepairs)
	}

	probeResponse, probeBody, latency, truncated, err := a.send(ctx, parsedURL, method, request, "", "")
	if err != nil {
		report.Checks = append(report.Checks,
			Check{Name: "reachability", Status: checkFailed, Weight: 10, Evidence: safeError(err)},
			Check{Name: "x402_challenge", Status: checkSkipped, Weight: 30, Evidence: "No HTTP response was received."},
			Check{Name: "payment_budget", Status: checkSkipped, Weight: 10, Evidence: "No payment challenge was available."},
			Check{Name: "paid_delivery", Status: checkSkipped, Weight: 25, Evidence: "Payment was not attempted."},
			Check{Name: "task_outcome", Status: checkSkipped, Weight: 25, Evidence: "No response was available to evaluate."},
		)
		return a.complete(&report, specInspection, parsedURL, nil)
	}
	defer probeResponse.Body.Close()
	report.Probe = ProbeResult{
		StatusCode:        probeResponse.StatusCode,
		LatencyMS:         latency.Milliseconds(),
		ContentType:       mediaType(probeResponse.Header.Get("Content-Type")),
		BodyBytes:         len(probeBody),
		BodySHA256:        bodyDigest(probeBody),
		ResponseTruncated: truncated,
	}
	report.Checks = append(report.Checks, Check{
		Name: "reachability", Status: checkPassed, Weight: 10, Points: 10,
		Evidence: fmt.Sprintf("Target responded with HTTP %d in %d ms.", probeResponse.StatusCode, latency.Milliseconds()),
	})

	if probeResponse.StatusCode != http.StatusPaymentRequired {
		report.Checks = append(report.Checks,
			Check{Name: "x402_challenge", Status: checkFailed, Weight: 30, Evidence: fmt.Sprintf("Expected HTTP 402 but received HTTP %d.", probeResponse.StatusCode)},
			Check{Name: "payment_budget", Status: checkSkipped, Weight: 10, Evidence: "No x402 payment option was available."},
			Check{Name: "paid_delivery", Status: checkSkipped, Weight: 25, Evidence: "Payment was not attempted."},
		)
		a.evaluateOutcome(ctx, &report, request, probeResponse.StatusCode, report.Probe.ContentType, probeBody)
		return a.complete(&report, specInspection, parsedURL, nil)
	}

	challenge, challengeTransport, authorizationHeader, err := ParsePaymentChallengeResponse(probeResponse.Header, probeBody)
	if err != nil {
		report.Checks = append(report.Checks,
			Check{Name: "x402_challenge", Status: checkFailed, Weight: 30, Evidence: err.Error()},
			Check{Name: "payment_budget", Status: checkSkipped, Weight: 10, Evidence: "The payment challenge was invalid."},
			Check{Name: "paid_delivery", Status: checkSkipped, Weight: 25, Evidence: "Payment was not attempted."},
			Check{Name: "task_outcome", Status: checkSkipped, Weight: 25, Evidence: "No paid response was available to evaluate."},
		)
		return a.complete(&report, specInspection, parsedURL, nil)
	}
	report.Probe.X402Version = challenge.Version
	report.Probe.ChallengeTransport = challengeTransport
	for _, option := range challenge.Accepts {
		report.Probe.PaymentOptions = append(report.Probe.PaymentOptions, option.Summary())
	}
	report.Checks = append(report.Checks, Check{
		Name: "x402_challenge", Status: checkPassed, Weight: 30, Points: 30,
		Evidence: fmt.Sprintf("Valid x402 v%d challenge advertised %d payment option(s).", challenge.Version, len(challenge.Accepts)),
	})

	if !request.Pay {
		report.Checks = append(report.Checks,
			Check{Name: "payment_budget", Status: checkSkipped, Weight: 10, Evidence: "Probe-only audit; no spend was authorized."},
			Check{Name: "paid_delivery", Status: checkSkipped, Weight: 25, Evidence: "Probe-only audit; payment was not attempted."},
			Check{Name: "task_outcome", Status: checkSkipped, Weight: 25, Evidence: "A paid response is required for outcome evaluation."},
		)
		return a.complete(&report, specInspection, parsedURL, &challenge)
	}

	selected, err := SelectPayment(challenge.Accepts, request, a.authorizer.policy)
	if err != nil {
		report.Checks = append(report.Checks,
			Check{Name: "payment_budget", Status: checkFailed, Weight: 10, Evidence: err.Error()},
			Check{Name: "paid_delivery", Status: checkSkipped, Weight: 25, Evidence: "No safe payment option was authorized."},
			Check{Name: "task_outcome", Status: checkSkipped, Weight: 25, Evidence: "No paid response was available to evaluate."},
		)
		return a.complete(&report, specInspection, parsedURL, &challenge)
	}
	summary := selected.Summary()
	report.Payment.Selected = &summary
	report.Checks = append(report.Checks, Check{
		Name: "payment_budget", Status: checkPassed, Weight: 10, Points: 10,
		Evidence: fmt.Sprintf("Authorized at most %s atomic units on %s; selected price is %s.", request.MaxPaymentAtomic, selected.Network, selected.Amount),
	})

	authorization, err := a.authorizer.Authorize(ctx, selected, challenge.Extensions)
	if err != nil {
		report.Payment.Error = safeError(err)
		report.Checks = append(report.Checks,
			Check{Name: "paid_delivery", Status: checkFailed, Weight: 25, Evidence: "Could not authorize payment: " + safeError(err)},
			Check{Name: "task_outcome", Status: checkSkipped, Weight: 25, Evidence: "No paid response was available to evaluate."},
		)
		return a.complete(&report, specInspection, parsedURL, &challenge)
	}
	report.Payment.Attempted = true
	report.Payment.Payer = authorization.Payer
	report.Payment.AuthorizationHeader = authorizationHeader
	paidResponse, paidBody, paidLatency, paidTruncated, err := a.send(ctx, parsedURL, method, request, authorizationHeader, authorization.Header)
	if err != nil {
		report.Payment.Error = safeError(err)
		report.Checks = append(report.Checks,
			Check{Name: "paid_delivery", Status: checkFailed, Weight: 25, Evidence: "Paid request failed before an HTTP response: " + safeError(err)},
			Check{Name: "task_outcome", Status: checkSkipped, Weight: 25, Evidence: "No paid response was available to evaluate."},
		)
		return a.complete(&report, specInspection, parsedURL, &challenge)
	}
	defer paidResponse.Body.Close()
	report.Tier = "verified"
	report.Payment.StatusCode = paidResponse.StatusCode
	report.Payment.LatencyMS = paidLatency.Milliseconds()
	report.Payment.ContentType = mediaType(paidResponse.Header.Get("Content-Type"))
	report.Payment.BodyBytes = len(paidBody)
	report.Payment.BodySHA256 = bodyDigest(paidBody)
	report.Payment.ResponseTruncated = paidTruncated
	report.Payment.SettlementEvidence = settlementEvidence(firstHeader(paidResponse.Header, "PAYMENT-RESPONSE", "X-PAYMENT-RESPONSE"))
	if paidResponse.StatusCode >= 200 && paidResponse.StatusCode < 300 {
		evidence := fmt.Sprintf("Paid retry returned HTTP %d in %d ms.", paidResponse.StatusCode, paidLatency.Milliseconds())
		if report.Payment.SettlementEvidence != "" {
			evidence += " Settlement evidence was returned."
		}
		report.Checks = append(report.Checks, Check{Name: "paid_delivery", Status: checkPassed, Weight: 25, Points: 25, Evidence: evidence})
	} else {
		report.Checks = append(report.Checks, Check{
			Name: "paid_delivery", Status: checkFailed, Weight: 25,
			Evidence: fmt.Sprintf("Paid retry returned HTTP %d; verify settlement evidence before retrying.", paidResponse.StatusCode),
		})
	}
	a.evaluateOutcome(ctx, &report, request, paidResponse.StatusCode, report.Payment.ContentType, paidBody)
	return a.complete(&report, specInspection, parsedURL, &challenge)
}

func (a *Auditor) validateRequest(ctx context.Context, request AuditRequest) (*url.URL, string, error) {
	method := strings.ToUpper(strings.TrimSpace(request.Method))
	if method == "" {
		method = http.MethodGet
	}
	if method != http.MethodGet && method != http.MethodPost {
		return nil, "", &ValidationError{Message: "method must be GET or POST"}
	}
	if int64(len(request.Body)) > a.config.MaxRequestBodyBytes {
		return nil, "", &ValidationError{Message: "body exceeds the 64 KiB limit"}
	}
	if method == http.MethodGet && len(request.Body) > 0 {
		return nil, "", &ValidationError{Message: "body is only supported with POST"}
	}
	if len(request.Body) > 0 && request.ContentType != "" && request.ContentType != "application/json" {
		return nil, "", &ValidationError{Message: "content_type must be application/json"}
	}
	if len(request.Expectation) > a.config.MaxExpectationBytes {
		return nil, "", &ValidationError{Message: "expectation exceeds the 2000-byte limit"}
	}
	if request.ExpectedStatus < 0 || request.ExpectedStatus > 599 {
		return nil, "", &ValidationError{Message: "expected_status must be a valid HTTP status code"}
	}
	if len(request.Body) > 0 && !json.Valid(request.Body) {
		return nil, "", &ValidationError{Message: "body must be valid JSON"}
	}
	if request.GenerateRepairs && !request.SpecReview {
		return nil, "", &ValidationError{Message: "generate_repairs requires spec_review to be true"}
	}
	parsed, err := a.target.ValidateURL(ctx, strings.TrimSpace(request.URL))
	if err != nil {
		return nil, "", &ValidationError{Message: err.Error()}
	}
	return parsed, method, nil
}

func (a *Auditor) send(ctx context.Context, target *url.URL, method string, auditRequest AuditRequest, paymentHeaderName, paymentHeaderValue string) (*http.Response, []byte, time.Duration, bool, error) {
	var body io.Reader
	if len(auditRequest.Body) > 0 {
		body = bytes.NewReader(auditRequest.Body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, nil, 0, false, fmt.Errorf("create target request: %w", err)
	}
	req.Header.Set("User-Agent", "Canary402/0.1 (+https://docs.obol.org/obol-stack/obol-stack)")
	req.Header.Set("Accept", "application/json")
	if len(auditRequest.Body) > 0 {
		contentType := strings.TrimSpace(auditRequest.ContentType)
		if contentType == "" {
			contentType = "application/json"
		}
		if contentType != "application/json" {
			return nil, nil, 0, false, fmt.Errorf("only application/json request bodies are supported")
		}
		req.Header.Set("Content-Type", contentType)
	}
	if paymentHeaderValue != "" {
		if paymentHeaderName != "PAYMENT-SIGNATURE" && paymentHeaderName != "X-PAYMENT" {
			return nil, nil, 0, false, fmt.Errorf("unsupported payment authorization header")
		}
		req.Header.Set(paymentHeaderName, paymentHeaderValue)
	}
	started := time.Now()
	resp, err := a.target.Do(req)
	latency := time.Since(started)
	if err != nil {
		return nil, nil, latency, false, err
	}
	limit := a.target.policy.MaxResponseBytes
	reader := io.LimitReader(resp.Body, limit+1)
	responseBody, readErr := io.ReadAll(reader)
	if readErr != nil {
		resp.Body.Close()
		return nil, nil, latency, false, fmt.Errorf("read target response: %w", readErr)
	}
	truncated := int64(len(responseBody)) > limit
	if truncated {
		responseBody = responseBody[:limit]
	}
	resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(responseBody))
	return resp, responseBody, latency, truncated, nil
}

func (a *Auditor) evaluateOutcome(ctx context.Context, report *AuditReport, request AuditRequest, status int, contentType string, body []byte) {
	statusPassed := (request.ExpectedStatus == 0 && status >= 200 && status < 300) || request.ExpectedStatus == status
	if !statusPassed {
		expected := "a 2xx response"
		if request.ExpectedStatus != 0 {
			expected = fmt.Sprintf("HTTP %d", request.ExpectedStatus)
		}
		report.Checks = append(report.Checks, Check{Name: "task_outcome", Status: checkFailed, Weight: 25, Evidence: fmt.Sprintf("Expected %s but received HTTP %d.", expected, status)})
		return
	}
	if strings.TrimSpace(request.Expectation) == "" {
		report.Checks = append(report.Checks, Check{Name: "task_outcome", Status: checkPassed, Weight: 25, Points: 25, Evidence: "Response status matched; no semantic expectation was supplied."})
		return
	}
	if a.evaluator == nil {
		report.Semantic.Error = "semantic evaluator is not configured"
		report.Checks = append(report.Checks, Check{Name: "task_outcome", Status: checkWarning, Weight: 25, Points: 12, Evidence: "HTTP status matched, but semantic evaluation was unavailable."})
		return
	}
	semanticInput := preview(body, a.config.SemanticInputBytes)
	result, err := a.evaluator.Evaluate(ctx, request.Expectation, semanticInput, status, contentType)
	if err != nil {
		report.Semantic = SemanticResult{Attempted: true, Error: safeError(err)}
		report.Checks = append(report.Checks, Check{Name: "task_outcome", Status: checkWarning, Weight: 25, Points: 12, Evidence: "HTTP status matched, but semantic evaluation failed: " + safeError(err)})
		return
	}
	report.Semantic = result
	report.Checks = append(report.Checks, semanticOutcomeCheck(result))
}

const semanticPassScoreFloor = 50

func semanticOutcomeCheck(result SemanticResult) Check {
	check := Check{
		Name:     "task_outcome",
		Status:   checkFailed,
		Weight:   25,
		Points:   result.Score * 25 / 100,
		Evidence: result.Reason,
	}
	if !result.Passed {
		return check
	}
	if result.Score < semanticPassScoreFloor {
		check.Status = checkWarning
		check.Evidence = fmt.Sprintf("Evaluator marked the outcome as passed but assigned only %d/100: %s", result.Score, result.Reason)
		return check
	}
	check.Status = checkPassed
	return check
}

func (a *Auditor) persist(report AuditReport) (AuditReport, error) {
	if err := a.store.Save(report); err != nil {
		return AuditReport{}, err
	}
	return report, nil
}

func (a *Auditor) complete(report *AuditReport, inspection *specInspection, target *url.URL, challenge *PaymentChallenge) (AuditReport, error) {
	if inspection != nil {
		checks, result := inspection.finalize(target, challenge, report.Method)
		report.SpecReview = &result
		report.Checks = append(report.Checks, checks...)
	}
	finalizeReport(report)
	return a.persist(*report)
}

func finalizeReport(report *AuditReport) {
	totalWeight, assessedWeight, points := 0, 0, 0
	hasFailure, hasWarning := false, false
	for _, check := range report.Checks {
		totalWeight += check.Weight
		if check.Status != checkSkipped {
			assessedWeight += check.Weight
			points += check.Points
		}
		if check.Status == checkFailed {
			hasFailure = true
		}
		if check.Status == checkWarning {
			hasWarning = true
		}
	}
	if assessedWeight > 0 {
		report.Score = points * 100 / assessedWeight
	}
	if totalWeight > 0 {
		report.CoveragePercent = assessedWeight * 100 / totalWeight
	}
	switch {
	case hasFailure:
		report.Verdict = "FAIL"
		report.Summary = "One or more required checks failed. Review the evidence before paying or retrying."
	case report.Tier == "probe" && hasWarning:
		report.Verdict = "PROBE_PASS_WITH_WARNINGS"
		report.Summary = "The endpoint returned a valid x402 challenge without payment, but its public service contract needs repair."
	case report.Tier == "probe":
		report.Verdict = "PROBE_PASS"
		report.Summary = "The endpoint returned a valid x402 challenge; no payment was made."
	case hasWarning:
		report.Verdict = "PASS_WITH_WARNINGS"
		report.Summary = "The paid request completed, with one or more evaluation warnings."
	default:
		report.Verdict = "PASS"
		report.Summary = "The x402 challenge, paid delivery, and requested outcome passed."
	}
}

func newReportID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func mediaType(header string) string {
	value, _, err := mime.ParseMediaType(header)
	if err != nil {
		return truncateText(header, 100)
	}
	return value
}

func preview(body []byte, limit int) string {
	if limit <= 0 || len(body) == 0 {
		return ""
	}
	if len(body) > limit {
		body = body[:limit]
	}
	if !utf8.Valid(body) {
		return "[binary response omitted]"
	}
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' || r >= 32 {
			return r
		}
		return -1
	}, string(body))
}

func truncateText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}

func safeError(err error) string {
	if err == nil {
		return ""
	}
	return truncateText(err.Error(), 400)
}

func settlementEvidence(header string) string {
	return truncateText(header, 2_000)
}

func firstHeader(headers http.Header, names ...string) string {
	for _, name := range names {
		if value := headers.Get(name); value != "" {
			return value
		}
	}
	return ""
}

func bodyDigest(body []byte) string {
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}
