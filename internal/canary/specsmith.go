package canary

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	maxSpecDocumentBytes = 256 << 10
	maxSpecFindings      = 64
	maxSchemaDepth       = 6
	maxSchemaProperties  = 64
	maxSchemaArrayItems  = 16
)

type specInspection struct {
	result SpecReviewResult
}

type fetchedSpecDocument struct {
	report SpecDocument
	body   []byte
}

func (a *Auditor) inspectSpecifications(ctx context.Context, target *url.URL, method string, requestBody json.RawMessage, generateRepairs bool) *specInspection {
	inspection := &specInspection{result: SpecReviewResult{Requested: true}}
	origin := &url.URL{Scheme: target.Scheme, Host: target.Host}

	openapiDoc := a.fetchSpecDocument(ctx, origin.ResolveReference(&url.URL{Path: "/openapi.json"}), "openapi")
	registrationDoc := a.fetchSpecDocument(ctx, origin.ResolveReference(&url.URL{Path: "/.well-known/agent-registration.json"}), "agent_registration")
	skillDoc := a.fetchSpecDocument(ctx, origin.ResolveReference(&url.URL{Path: "/skill.md"}), "skill")

	inspection.result.Documents = []SpecDocument{openapiDoc.report, registrationDoc.report, skillDoc.report}
	var openapi map[string]any
	if openapiDoc.report.Available && !openapiDoc.report.ResponseTruncated {
		parsed, err := decodeJSONObject(openapiDoc.body)
		if err != nil {
			inspection.addFinding("openapi_invalid", "error", "The public OpenAPI document is not valid JSON object data.", "Publish a valid OpenAPI 3.1 JSON document at /openapi.json.")
		} else if version := stringValue(parsed["openapi"]); !strings.HasPrefix(version, "3.") {
			inspection.addFinding("openapi_version", "error", "The public document does not declare an OpenAPI 3.x version.", "Set openapi to 3.1.0 and validate the document before publishing it.")
		} else {
			openapi = parsed
			inspection.result.Documents[0].Valid = true
			inspection.analyzeOpenAPI(openapi, target, method)
		}
	} else if openapiDoc.report.ResponseTruncated {
		inspection.addFinding("openapi_too_large", "error", "The public OpenAPI document exceeds Canary402's 256 KiB inspection limit.", "Publish a smaller operation-focused OpenAPI document.")
	} else {
		inspection.addFinding("openapi_missing", "warning", "No usable public OpenAPI document was found at /openapi.json.", "Publish an OpenAPI 3.1 JSON document containing the paid operation.")
	}

	inspection.analyzeRegistration(target, registrationDoc)
	inspection.analyzeSkillDocument(skillDoc)
	if generateRepairs {
		inspection.result.Repairs = buildSpecRepairs(target, method, requestBody)
	}
	return inspection
}

func (a *Auditor) fetchSpecDocument(ctx context.Context, target *url.URL, kind string) fetchedSpecDocument {
	report := SpecDocument{Kind: kind, URL: redactedURL(target)}
	validated, err := a.target.ValidateURL(ctx, target.String())
	if err != nil {
		report.Error = safeError(err)
		return fetchedSpecDocument{report: report}
	}
	timeout := 5 * time.Second
	if a.target.policy.Timeout > 0 && a.target.policy.Timeout < timeout {
		timeout = a.target.policy.Timeout
	}
	documentCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(documentCtx, http.MethodGet, validated.String(), nil)
	if err != nil {
		report.Error = "could not create discovery request"
		return fetchedSpecDocument{report: report}
	}
	req.Header.Set("User-Agent", "Canary402/0.2 (+https://docs.obol.org/obol-stack/obol-stack)")
	req.Header.Set("Accept", "application/json, text/markdown;q=0.8, text/plain;q=0.5")
	resp, err := a.target.Do(req)
	if err != nil {
		report.Error = safeError(err)
		return fetchedSpecDocument{report: report}
	}
	defer resp.Body.Close()
	report.StatusCode = resp.StatusCode
	report.ContentType = mediaType(resp.Header.Get("Content-Type"))
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSpecDocumentBytes+1))
	if err != nil {
		report.Error = "could not read discovery document"
		return fetchedSpecDocument{report: report}
	}
	report.ResponseTruncated = len(body) > maxSpecDocumentBytes
	if report.ResponseTruncated {
		body = body[:maxSpecDocumentBytes]
	}
	report.BodyBytes = len(body)
	if len(body) > 0 {
		report.BodySHA256 = bodyDigest(body)
	}
	report.Available = resp.StatusCode == http.StatusOK
	return fetchedSpecDocument{report: report, body: body}
}

func decodeJSONObject(body []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var result map[string]any
	if err := decoder.Decode(&result); err != nil || result == nil {
		return nil, fmt.Errorf("invalid JSON object")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return nil, fmt.Errorf("multiple JSON values")
	}
	return result, nil
}

func (s *specInspection) analyzeOpenAPI(document map[string]any, target *url.URL, method string) {
	paths, ok := document["paths"].(map[string]any)
	if !ok {
		s.addFinding("openapi_paths_missing", "error", "The OpenAPI document has no paths object.", "Add the paid operation beneath the OpenAPI paths object.")
		return
	}
	path, pathItem := findOpenAPIPath(paths, target.Path)
	if pathItem == nil {
		s.addFinding("operation_missing", "error", "The requested paid route is not documented in OpenAPI.", "Add the requested route and method to /openapi.json.")
		return
	}
	operation, ok := pathItem[strings.ToLower(method)].(map[string]any)
	if !ok {
		s.addFinding("method_missing", "error", "The requested HTTP method is not documented for the matching OpenAPI path.", "Add the requested method beneath the matching OpenAPI path.")
		return
	}
	s.result.Operation.Found = true
	s.result.Operation.OpenAPIPath = path
	s.result.Operation.Method = method
	s.result.Operation.Summary = safeSpecText(stringValue(operation["summary"]), 240)
	if s.result.Operation.Summary == "" {
		s.addFinding("summary_missing", "warning", "The paid operation has no concise OpenAPI summary.", "Add a public summary that states what a buyer receives.")
	}

	requiresInput := method == http.MethodPost || target.RawQuery != ""
	if method == http.MethodPost {
		schema, example := requestBodySchema(document, operation)
		s.result.Operation.RequestSchema = schema != nil
		s.result.Operation.ConcreteRequestSchema = isConcreteSchema(document, schema, 0)
		s.result.Operation.RequestExample = example
	} else if requiresInput {
		hasParameters, concrete := queryParameterContract(operation)
		s.result.Operation.RequestSchema = hasParameters
		s.result.Operation.ConcreteRequestSchema = concrete
	} else {
		s.result.Operation.RequestSchema = true
		s.result.Operation.ConcreteRequestSchema = true
	}
	if !s.result.Operation.RequestSchema {
		s.addFinding("request_schema_missing", "error", "The paid operation does not document its request shape.", "Add an application/json request schema or explicit query parameters.")
	} else if !s.result.Operation.ConcreteRequestSchema {
		s.addFinding("request_schema_generic", "warning", "The request schema exists but is too generic for an autonomous buyer.", "Declare concrete properties, types, and required fields.")
	}
	if requiresInput && !s.result.Operation.RequestExample {
		s.addFinding("request_example_missing", "info", "No non-sensitive request example is published.", "Add an example that validates against the request schema.")
	}

	responseSchema, responseExample := successfulResponseSchema(document, operation)
	s.result.Operation.ResponseSchema = responseSchema != nil
	s.result.Operation.ConcreteResponseSchema = isConcreteSchema(document, responseSchema, 0)
	s.result.Operation.ResponseExample = responseExample
	if !s.result.Operation.ResponseSchema {
		s.addFinding("response_schema_missing", "warning", "The paid operation does not document a successful JSON response schema.", "Document the successful response without publishing confidential example values.")
	} else if !s.result.Operation.ConcreteResponseSchema {
		s.addFinding("response_schema_generic", "warning", "The successful response schema is too generic to validate automatically.", "Declare concrete response fields and types.")
	}
	if !s.result.Operation.ResponseExample {
		s.addFinding("response_example_missing", "info", "No non-sensitive successful response example is published.", "Add a redacted example that validates against the response schema.")
	}
}

func findOpenAPIPath(paths map[string]any, targetPath string) (string, map[string]any) {
	if targetPath == "" {
		targetPath = "/"
	}
	if exact, ok := paths[targetPath].(map[string]any); ok {
		return targetPath, exact
	}
	keys := make([]string, 0, len(paths))
	for key := range paths {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if pathTemplateMatches(key, targetPath) {
			if item, ok := paths[key].(map[string]any); ok {
				return key, item
			}
		}
	}
	return "", nil
}

func pathTemplateMatches(template, actual string) bool {
	if template == actual {
		return true
	}
	templateParts := strings.Split(strings.Trim(template, "/"), "/")
	actualParts := strings.Split(strings.Trim(actual, "/"), "/")
	if len(templateParts) != len(actualParts) {
		return false
	}
	for index := range templateParts {
		part := templateParts[index]
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") && len(part) > 2 {
			if actualParts[index] == "" {
				return false
			}
			continue
		}
		if part != actualParts[index] {
			return false
		}
	}
	return true
}

func requestBodySchema(document, operation map[string]any) (map[string]any, bool) {
	requestBody, _ := resolveObject(document, operation["requestBody"], 0)
	content, _ := requestBody["content"].(map[string]any)
	media := jsonMedia(content)
	if media == nil {
		return nil, false
	}
	schema, _ := resolveObject(document, media["schema"], 0)
	return schema, hasExample(media) || hasExample(schema)
}

func successfulResponseSchema(document, operation map[string]any) (map[string]any, bool) {
	responses, _ := operation["responses"].(map[string]any)
	if responses == nil {
		return nil, false
	}
	keys := make([]string, 0, len(responses))
	for key := range responses {
		if strings.HasPrefix(key, "2") || key == "default" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		response, _ := resolveObject(document, responses[key], 0)
		content, _ := response["content"].(map[string]any)
		media := jsonMedia(content)
		if media == nil {
			continue
		}
		schema, _ := resolveObject(document, media["schema"], 0)
		return schema, hasExample(media) || hasExample(schema)
	}
	return nil, false
}

func jsonMedia(content map[string]any) map[string]any {
	if content == nil {
		return nil
	}
	if media, ok := content["application/json"].(map[string]any); ok {
		return media
	}
	keys := make([]string, 0, len(content))
	for key := range content {
		if strings.HasSuffix(strings.ToLower(key), "+json") {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return nil
	}
	media, _ := content[keys[0]].(map[string]any)
	return media
}

func resolveObject(document map[string]any, raw any, depth int) (map[string]any, bool) {
	object, ok := raw.(map[string]any)
	if !ok || depth > 8 {
		return nil, false
	}
	ref := stringValue(object["$ref"])
	if ref == "" {
		return object, true
	}
	if !strings.HasPrefix(ref, "#/") {
		return object, true
	}
	var current any = document
	for _, token := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		container, ok := current.(map[string]any)
		if !ok {
			return object, true
		}
		token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		current, ok = container[token]
		if !ok {
			return object, true
		}
	}
	return resolveObject(document, current, depth+1)
}

func isConcreteSchema(document map[string]any, schema map[string]any, depth int) bool {
	if schema == nil || depth > 8 {
		return false
	}
	resolved, _ := resolveObject(document, schema, depth)
	if resolved == nil {
		return false
	}
	for _, keyword := range []string{"oneOf", "anyOf", "allOf"} {
		if variants, ok := resolved[keyword].([]any); ok {
			for _, variant := range variants {
				candidate, _ := resolveObject(document, variant, depth+1)
				if isConcreteSchema(document, candidate, depth+1) {
					return true
				}
			}
		}
	}
	typeName := stringValue(resolved["type"])
	switch typeName {
	case "object":
		properties, _ := resolved["properties"].(map[string]any)
		return len(properties) > 0
	case "array":
		items, _ := resolveObject(document, resolved["items"], depth+1)
		return isConcreteSchema(document, items, depth+1)
	case "string", "number", "integer", "boolean", "null":
		return true
	}
	_, hasEnum := resolved["enum"]
	_, hasConst := resolved["const"]
	return hasEnum || hasConst
}

func queryParameterContract(operation map[string]any) (bool, bool) {
	parameters, _ := operation["parameters"].([]any)
	found := false
	for _, raw := range parameters {
		parameter, _ := raw.(map[string]any)
		if stringValue(parameter["in"]) != "query" {
			continue
		}
		found = true
		if stringValue(parameter["name"]) == "" {
			return true, false
		}
		if _, ok := parameter["schema"].(map[string]any); !ok {
			return true, false
		}
	}
	return found, found
}

func hasExample(value map[string]any) bool {
	if value == nil {
		return false
	}
	if _, ok := value["example"]; ok {
		return true
	}
	if examples, ok := value["examples"].(map[string]any); ok && len(examples) > 0 {
		return true
	}
	return false
}

func (s *specInspection) analyzeRegistration(target *url.URL, document fetchedSpecDocument) {
	if !document.report.Available {
		s.addFinding("agent_registration_missing", "info", "No ERC-8004 registration document was found at the standard well-known path.", "Publish the registration document when the service has an ERC-8004 identity.")
		return
	}
	var registration struct {
		Active        *bool `json:"active"`
		Registrations []struct {
			AgentID any `json:"agentId"`
		} `json:"registrations"`
		Services []struct {
			Endpoint string `json:"endpoint"`
		} `json:"services"`
	}
	if json.Unmarshal(document.body, &registration) != nil {
		s.addFinding("agent_registration_invalid", "warning", "The agent registration document is not valid JSON.", "Publish a valid agent-registration.json document.")
		return
	}
	s.result.Documents[1].Valid = true
	if registration.Active != nil && !*registration.Active {
		s.addFinding("agent_registration_inactive", "warning", "The agent registration document marks the service inactive.", "Activate the registration or remove stale service endpoints.")
	}
	listed := false
	for _, service := range registration.Services {
		endpoint, err := url.ParseRequestURI(strings.TrimSpace(service.Endpoint))
		if err != nil || endpoint.Scheme != target.Scheme || !strings.EqualFold(endpoint.Host, target.Host) {
			continue
		}
		path := strings.TrimSuffix(endpoint.Path, "/")
		if path == "" || target.Path == path || strings.HasPrefix(target.Path, path+"/") {
			listed = true
			break
		}
	}
	if !listed {
		s.addFinding("service_not_registered", "warning", "The target route is not covered by an endpoint in agent-registration.json.", "Add a public service endpoint that is an origin/path prefix of the paid route.")
	}
	if len(registration.Registrations) == 0 {
		s.addFinding("agent_id_missing", "info", "The registration document contains no on-chain Agent ID.", "Add registrations after the identity is minted.")
	}
}

func (s *specInspection) analyzeSkillDocument(document fetchedSpecDocument) {
	if !document.report.Available {
		s.addFinding("skill_document_missing", "info", "No public skill.md document was found.", "Publish concise agent-facing usage guidance when appropriate.")
		return
	}
	if document.report.ResponseTruncated || len(bytes.TrimSpace(document.body)) == 0 {
		s.addFinding("skill_document_invalid", "warning", "The public skill document is empty or exceeds the inspection limit.", "Publish a concise text/Markdown skill document.")
		return
	}
	s.result.Documents[2].Valid = true
}

func (s *specInspection) finalize(target *url.URL, challenge *PaymentChallenge, method string) ([]Check, SpecReviewResult) {
	if challenge != nil {
		s.analyzeChallenge(target, challenge, method)
	} else {
		s.addFinding("challenge_metadata_unavailable", "info", "No valid x402 challenge was available for metadata comparison.", "Fix the live 402 challenge before relying on discovery metadata.")
	}
	s.result.Status = "READY"
	for _, finding := range s.result.Findings {
		if finding.Severity == "error" || finding.Severity == "warning" {
			s.result.Status = "NEEDS_REPAIR"
			break
		}
	}
	discoveryReady := s.result.Documents[0].Valid && s.result.Operation.Found
	discoveryCheck := Check{Name: "service_discovery", Status: checkWarning, Weight: 15, Points: 7, Evidence: "Public discovery is missing, invalid, or does not document the requested operation."}
	if discoveryReady {
		discoveryCheck = Check{Name: "service_discovery", Status: checkPassed, Weight: 15, Points: 15, Evidence: "OpenAPI 3.x documents the requested operation."}
	}
	contractReady := s.result.Operation.ConcreteRequestSchema && s.result.Operation.ConcreteResponseSchema && s.result.Challenge.ResourceMatches && s.result.Challenge.BazaarExtension
	if method == http.MethodPost {
		contractReady = contractReady && s.result.Challenge.BazaarInputSchema
	}
	contractCheck := Check{Name: "request_contract", Status: checkWarning, Weight: 15, Points: 7, Evidence: "The operation contract or x402 discovery metadata needs repair; generated artifacts are proposals requiring review."}
	if contractReady {
		contractCheck = Check{Name: "request_contract", Status: checkPassed, Weight: 15, Points: 15, Evidence: "Request, response, resource URL, and Bazaar metadata are concrete enough for an autonomous buyer."}
	}
	return []Check{discoveryCheck, contractCheck}, s.result
}

func (s *specInspection) analyzeChallenge(target *url.URL, challenge *PaymentChallenge, method string) {
	advertised := strings.TrimSpace(challenge.ResourceURL)
	if advertised == "" {
		s.addFinding("challenge_resource_missing", "warning", "The x402 challenge does not advertise a resource URL.", "Include the exact externally requested HTTPS resource URL in the challenge.")
	} else {
		resource, err := url.ParseRequestURI(advertised)
		if err != nil || resource.Scheme == "" || resource.Host == "" || resource.User != nil {
			s.addFinding("challenge_resource_invalid", "error", "The x402 challenge advertises an invalid resource URL.", "Advertise the exact public HTTPS resource URL.")
		} else {
			s.result.Challenge.ResourceURL = redactedURL(resource)
			s.result.Challenge.ResourceMatches = sameResource(target, resource)
			if resource.Scheme != "https" && target.Scheme == "https" {
				s.addFinding("challenge_resource_insecure", "error", "The x402 challenge advertises HTTP for an externally HTTPS resource.", "Preserve X-Forwarded-Proto and generate an https:// resource URL.")
			} else if !s.result.Challenge.ResourceMatches {
				s.addFinding("challenge_resource_mismatch", "error", "The x402 challenge resource does not exactly match the requested URL.", "Build the challenge from the original external URL without rewriting its host, path, or query.")
			}
		}
	}
	bazaar, ok := challenge.Extensions["bazaar"].(map[string]any)
	s.result.Challenge.BazaarExtension = ok && len(bazaar) > 0
	if !s.result.Challenge.BazaarExtension {
		s.addFinding("bazaar_extension_missing", "warning", "The challenge has no x402 Bazaar discovery extension.", "Declare Bazaar metadata with input/output schemas and a non-sensitive example.")
		return
	}
	info, _ := bazaar["info"].(map[string]any)
	s.result.Challenge.BazaarInputSchema = info != nil && info["input"] != nil || containsNestedKey(bazaar, "inputSchema", 0)
	s.result.Challenge.BazaarOutputSchema = info != nil && info["output"] != nil || containsNestedKey(bazaar, "outputSchema", 0)
	if method == http.MethodPost && !s.result.Challenge.BazaarInputSchema {
		s.addFinding("bazaar_input_missing", "warning", "Bazaar metadata does not describe a POST input.", "Declare bodyType=json, inputSchema, and a validating non-sensitive input example.")
	}
	if !s.result.Challenge.BazaarOutputSchema {
		s.addFinding("bazaar_output_missing", "info", "Bazaar metadata does not describe the successful output.", "Declare a redacted output schema or example.")
	}
}

func sameResource(expected, advertised *url.URL) bool {
	return strings.EqualFold(expected.Scheme, advertised.Scheme) &&
		strings.EqualFold(expected.Host, advertised.Host) &&
		canonicalResourcePath(expected) == canonicalResourcePath(advertised) &&
		expected.RawQuery == advertised.RawQuery
}

func canonicalResourcePath(value *url.URL) string {
	path := value.EscapedPath()
	if path == "" {
		return "/"
	}
	return path
}

func containsNestedKey(value any, wanted string, depth int) bool {
	if depth > 6 {
		return false
	}
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if key == wanted || containsNestedKey(typed[key], wanted, depth+1) {
				return true
			}
		}
	case []any:
		limit := len(typed)
		if limit > 64 {
			limit = 64
		}
		for _, item := range typed[:limit] {
			if containsNestedKey(item, wanted, depth+1) {
				return true
			}
		}
	}
	return false
}

func (s *specInspection) addFinding(code, severity, message, repair string) {
	if len(s.result.Findings) >= maxSpecFindings {
		return
	}
	for _, finding := range s.result.Findings {
		if finding.Code == code {
			return
		}
	}
	s.result.Findings = append(s.result.Findings, SpecFinding{Code: code, Severity: severity, Message: message, Repair: repair})
}

func buildSpecRepairs(target *url.URL, method string, requestBody json.RawMessage) *SpecRepairBundle {
	inputSchema := map[string]any{"type": "object", "additionalProperties": true, "description": "Replace with the reviewed request schema."}
	if method == http.MethodPost && len(requestBody) > 0 {
		if inferred := inferJSONSchema(requestBody); inferred != nil {
			inputSchema = inferred
			inputSchema["description"] = "Shape inferred from the caller-supplied audit request; review required fields and semantics before publishing."
		}
	}
	outputSchema := map[string]any{"type": "object", "additionalProperties": true, "description": "Replace with a schema derived from a reviewed, non-confidential successful response."}
	operation := map[string]any{
		"summary": "TODO: describe what the paid operation returns",
		"responses": map[string]any{
			"200": map[string]any{
				"description": "Successful paid response",
				"content":     map[string]any{"application/json": map[string]any{"schema": outputSchema}},
			},
		},
	}
	if method == http.MethodPost {
		operation["requestBody"] = map[string]any{
			"required": true,
			"content":  map[string]any{"application/json": map[string]any{"schema": inputSchema}},
		}
	}
	path := target.EscapedPath()
	if path == "" {
		path = "/"
	}
	bazaar := map[string]any{
		"inputSchema": inputSchema,
		"output":      map[string]any{"schema": outputSchema},
	}
	if method == http.MethodPost {
		bazaar["bodyType"] = "json"
	}
	template := SpecRequestTemplate{URL: redactedURL(target), Method: method}
	if method == http.MethodPost {
		template.ContentType = "application/json"
		template.BodyFile = "request.json"
	}
	return &SpecRepairBundle{
		OpenAPIPatch:      map[string]any{"paths": map[string]any{path: map[string]any{strings.ToLower(method): operation}}},
		BazaarDeclaration: bazaar,
		RequestTemplate:   template,
		ReviewRequired: []string{
			"Replace every TODO and generic output schema with reviewed service semantics.",
			"Decide which inferred input properties are required; Canary402 does not infer requiredness.",
			"Add non-sensitive examples that validate against the final schemas.",
			"Validate and merge these proposed fragments into the seller's source specification.",
		},
	}
}

func inferJSONSchema(raw json.RawMessage) map[string]any {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return nil
	}
	return schemaForValue(value, 0)
}

func schemaForValue(value any, depth int) map[string]any {
	if depth >= maxSchemaDepth {
		return map[string]any{}
	}
	switch typed := value.(type) {
	case map[string]any:
		properties := make(map[string]any)
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		if len(keys) > maxSchemaProperties {
			keys = keys[:maxSchemaProperties]
		}
		for _, key := range keys {
			if len(key) > 128 {
				continue
			}
			properties[key] = schemaForValue(typed[key], depth+1)
		}
		return map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	case []any:
		limit := len(typed)
		if limit > maxSchemaArrayItems {
			limit = maxSchemaArrayItems
		}
		variants := make(map[string]map[string]any)
		for _, item := range typed[:limit] {
			schema := schemaForValue(item, depth+1)
			encoded, _ := json.Marshal(schema)
			variants[string(encoded)] = schema
		}
		items := map[string]any{}
		if len(variants) == 1 {
			for _, schema := range variants {
				items = schema
			}
		} else if len(variants) > 1 {
			keys := make([]string, 0, len(variants))
			for key := range variants {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			oneOf := make([]any, 0, len(keys))
			for _, key := range keys {
				oneOf = append(oneOf, variants[key])
			}
			items = map[string]any{"oneOf": oneOf}
		}
		return map[string]any{"type": "array", "items": items}
	case json.Number:
		if !strings.ContainsAny(typed.String(), ".eE") {
			return map[string]any{"type": "integer"}
		}
		return map[string]any{"type": "number"}
	case string:
		return map[string]any{"type": "string"}
	case bool:
		return map[string]any{"type": "boolean"}
	case nil:
		return map[string]any{"type": "null"}
	default:
		return map[string]any{}
	}
}

func safeSpecText(value string, limit int) string {
	return truncateText(preview([]byte(value), limit), limit)
}
