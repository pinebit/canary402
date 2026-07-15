package canary

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type HTTPHandlerConfig struct {
	MaxConcurrent int
	Version       string
	Logger        *slog.Logger
}

type apiHandler struct {
	auditor   *Auditor
	store     *FileStore
	semaphore chan struct{}
	version   string
	logger    *slog.Logger
	mux       *http.ServeMux
}

func NewHTTPHandler(auditor *Auditor, store *FileStore, config HTTPHandlerConfig) http.Handler {
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	h := &apiHandler{
		auditor: auditor, store: store, semaphore: make(chan struct{}, config.MaxConcurrent),
		version: config.Version, logger: config.Logger, mux: http.NewServeMux(),
	}
	h.mux.HandleFunc("GET /", h.landing)
	h.mux.HandleFunc("GET /health", h.health)
	h.mux.HandleFunc("GET /openapi.json", h.openapi)
	h.mux.HandleFunc("POST /audit", h.audit)
	h.mux.HandleFunc("GET /reports/{id}", h.report)
	return h
}

func (h *apiHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	defer func() {
		if recovered := recover(); recovered != nil {
			h.logger.Error("request panic", "panic", recovered, "method", r.Method, "path", r.URL.Path)
			writeError(w, http.StatusInternalServerError, "internal server error")
		}
		h.logger.Info("request", "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(started).Milliseconds())
	}()
	h.mux.ServeHTTP(w, r)
}

func (h *apiHandler) landing(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	const page = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Canary402</title><style>body{font:17px/1.5 system-ui;max-width:760px;margin:10vh auto;padding:0 24px;color:#17202a}code{background:#f3f4f6;padding:.15em .35em;border-radius:4px}h1{font-size:3rem;margin-bottom:0}.tag{color:#556}</style></head><body><h1>Canary402</h1><p class="tag">The mystery shopper for paid agents.</p><p>Submit an x402 endpoint to <code>POST /audit</code>. Canary402 validates its payment challenge, optionally makes one strictly budgeted payment, and publishes an evidence-backed report.</p><p><a href="/openapi.json">OpenAPI</a> · <a href="/health">Health</a></p></body></html>`
	io.WriteString(w, page)
}

func (h *apiHandler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok", "service": "canary402", "version": h.version,
		"semantic_evaluation": h.auditor.evaluator != nil,
	})
}

func (h *apiHandler) audit(w http.ResponseWriter, r *http.Request) {
	select {
	case h.semaphore <- struct{}{}:
		defer func() { <-h.semaphore }()
	default:
		writeError(w, http.StatusTooManyRequests, "audit capacity is currently full")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 96<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request AuditRequest
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request: "+decodeError(err))
		return
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		writeError(w, http.StatusBadRequest, "request must contain one JSON object")
		return
	}
	report, err := h.auditor.Audit(r.Context(), request)
	if err != nil {
		var validation *ValidationError
		if errors.As(err, &validation) {
			writeError(w, http.StatusBadRequest, validation.Message)
			return
		}
		h.logger.Error("audit failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not persist audit report")
		return
	}
	w.Header().Set("X-Canary402-Report-ID", report.ID)
	w.Header().Set("Location", "/reports/"+report.ID)
	writeJSON(w, http.StatusOK, report)
}

func (h *apiHandler) report(w http.ResponseWriter, r *http.Request) {
	report, err := h.store.Get(r.PathValue("id"))
	if errors.Is(err, ErrReportNotFound) {
		writeError(w, http.StatusNotFound, "report not found")
		return
	}
	if err != nil {
		h.logger.Error("read report", "error", err)
		writeError(w, http.StatusInternalServerError, "could not read report")
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *apiHandler) openapi(w http.ResponseWriter, _ *http.Request) {
	document := map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title": "Canary402", "version": h.version,
			"description": "A budget-capped, end-to-end mystery shopper for x402 services.",
		},
		"paths": map[string]any{
			"/audit": map[string]any{"post": map[string]any{
				"summary": "Audit an x402 service", "operationId": "auditService",
				"requestBody": map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/AuditRequest"}}}},
				"responses":   map[string]any{"200": map[string]any{"description": "Audit report"}, "400": map[string]any{"description": "Unsafe or invalid request"}},
			}},
			"/reports/{id}": map[string]any{"get": map[string]any{"summary": "Read a public audit report", "responses": map[string]any{"200": map[string]any{"description": "Audit report"}}}},
			"/health":       map[string]any{"get": map[string]any{"summary": "Health check", "responses": map[string]any{"200": map[string]any{"description": "Healthy"}}}},
		},
		"components": map[string]any{"schemas": map[string]any{"AuditRequest": map[string]any{
			"type": "object", "required": []string{"url"}, "additionalProperties": false,
			"properties": map[string]any{
				"url":                map[string]any{"type": "string", "format": "uri"},
				"method":             map[string]any{"type": "string", "enum": []string{"GET", "POST"}, "default": "GET"},
				"body":               map[string]any{"description": "Optional JSON body"},
				"content_type":       map[string]any{"type": "string", "enum": []string{"application/json"}},
				"expectation":        map[string]any{"type": "string", "maxLength": 2000},
				"expected_status":    map[string]any{"type": "integer", "minimum": 100, "maximum": 599},
				"pay":                map[string]any{"type": "boolean", "default": false},
				"max_payment_atomic": map[string]any{"type": "string", "pattern": "^[0-9]+$"},
				"payment_network":    map[string]any{"type": "string", "examples": []string{"base-sepolia", "base"}},
				"payment_asset":      map[string]any{"type": "string"},
			},
		}}},
	}
	writeJSON(w, http.StatusOK, document)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message, "status": status})
}

func decodeError(err error) string {
	message := err.Error()
	if strings.Contains(message, "http: request body too large") {
		return "request exceeds the 96 KiB limit"
	}
	return truncateText(message, 240)
}
