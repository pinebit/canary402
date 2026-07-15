package canary

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var addressPattern = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)
var signaturePattern = regexp.MustCompile(`^0x[0-9a-fA-F]{130}$`)

type PaymentChallenge struct {
	Version    int
	Accepts    []PaymentRequirement
	Extensions map[string]any
}

type PaymentRequirement struct {
	Scheme            string
	Network           string
	Amount            string
	Asset             string
	PayTo             string
	MaxTimeoutSeconds int
	Extra             map[string]any
}

func (p PaymentRequirement) Summary() PaymentOptionSummary {
	transfer, _ := p.Extra["assetTransferMethod"].(string)
	return PaymentOptionSummary{
		Scheme:         p.Scheme,
		Network:        p.Network,
		Amount:         p.Amount,
		Asset:          p.Asset,
		PayTo:          p.PayTo,
		TransferMethod: transfer,
	}
}

func ParsePaymentChallenge(body []byte) (PaymentChallenge, error) {
	var raw struct {
		X402Version int              `json:"x402Version"`
		Accepts     []map[string]any `json:"accepts"`
		Extensions  map[string]any   `json:"extensions"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		return PaymentChallenge{}, fmt.Errorf("402 response is not valid JSON")
	}
	if raw.X402Version != 2 {
		return PaymentChallenge{}, fmt.Errorf("unsupported x402 version %d; Canary402 currently supports v2", raw.X402Version)
	}
	if len(raw.Accepts) == 0 {
		return PaymentChallenge{}, fmt.Errorf("402 response has no payment options")
	}

	challenge := PaymentChallenge{Version: raw.X402Version, Extensions: raw.Extensions}
	for index, item := range raw.Accepts {
		amount, err := atomicString(item["amount"])
		if err != nil {
			amount, err = atomicString(item["maxAmountRequired"])
		}
		if err != nil {
			return PaymentChallenge{}, fmt.Errorf("payment option %d has no valid atomic amount", index+1)
		}
		extra, _ := item["extra"].(map[string]any)
		timeout := 60
		if value, ok := item["maxTimeoutSeconds"]; ok {
			parsed, parseErr := integerValue(value)
			if parseErr != nil || parsed < 1 || parsed > 86_400 {
				return PaymentChallenge{}, fmt.Errorf("payment option %d has invalid maxTimeoutSeconds", index+1)
			}
			timeout = parsed
		}
		challenge.Accepts = append(challenge.Accepts, PaymentRequirement{
			Scheme:            stringValue(item["scheme"]),
			Network:           stringValue(item["network"]),
			Amount:            amount,
			Asset:             stringValue(item["asset"]),
			PayTo:             stringValue(item["payTo"]),
			MaxTimeoutSeconds: timeout,
			Extra:             extra,
		})
	}
	return challenge, nil
}

func ParsePaymentChallengeResponse(headers http.Header, body []byte) (PaymentChallenge, string, string, error) {
	if encoded := strings.TrimSpace(headers.Get("PAYMENT-REQUIRED")); encoded != "" {
		decoded, err := decodePaymentHeader(encoded)
		if err != nil {
			return PaymentChallenge{}, "", "", fmt.Errorf("PAYMENT-REQUIRED header is not valid base64 JSON")
		}
		challenge, err := ParsePaymentChallenge(decoded)
		if err != nil {
			return PaymentChallenge{}, "", "", err
		}
		return challenge, "PAYMENT-REQUIRED", "PAYMENT-SIGNATURE", nil
	}
	challenge, err := ParsePaymentChallenge(body)
	if err != nil {
		return PaymentChallenge{}, "", "", err
	}
	return challenge, "json-body", "X-PAYMENT", nil
}

func decodePaymentHeader(value string) ([]byte, error) {
	if strings.HasPrefix(strings.TrimSpace(value), "{") {
		return []byte(value), nil
	}
	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	for _, encoding := range encodings {
		if decoded, err := encoding.DecodeString(value); err == nil {
			return decoded, nil
		}
	}
	return nil, fmt.Errorf("invalid base64")
}

func SelectPayment(requirements []PaymentRequirement, request AuditRequest, policy PaymentPolicy) (PaymentRequirement, error) {
	requestMax, ok := new(big.Int).SetString(strings.TrimSpace(request.MaxPaymentAtomic), 10)
	if !ok || requestMax.Sign() <= 0 {
		return PaymentRequirement{}, fmt.Errorf("max_payment_atomic must be a positive integer when pay is true")
	}
	systemMax, ok := new(big.Int).SetString(strings.TrimSpace(policy.MaxAtomicAmount), 10)
	if !ok || systemMax.Sign() <= 0 {
		return PaymentRequirement{}, fmt.Errorf("operator payment cap is invalid")
	}
	if requestMax.Cmp(systemMax) > 0 {
		return PaymentRequirement{}, fmt.Errorf("requested payment cap exceeds the operator limit of %s atomic units", systemMax.String())
	}

	wantedNetwork := ""
	if strings.TrimSpace(request.PaymentNetwork) != "" {
		network, _, err := normalizeNetwork(request.PaymentNetwork)
		if err != nil {
			return PaymentRequirement{}, err
		}
		wantedNetwork = network
	}
	wantedAsset := strings.ToLower(strings.TrimSpace(request.PaymentAsset))

	var matches []PaymentRequirement
	var reasons []string
	for _, requirement := range requirements {
		network, _, err := normalizeNetwork(requirement.Network)
		if err != nil {
			reasons = append(reasons, err.Error())
			continue
		}
		requirement.Network = network
		if _, allowed := policy.AllowedNetworks[network]; !allowed {
			reasons = append(reasons, "network "+network+" is not allowed")
			continue
		}
		if wantedNetwork != "" && network != wantedNetwork {
			continue
		}
		if requirement.Scheme != "exact" {
			reasons = append(reasons, "only the exact payment scheme is supported")
			continue
		}
		transfer, _ := requirement.Extra["assetTransferMethod"].(string)
		if transfer != "" && transfer != "eip3009" {
			reasons = append(reasons, "payment method "+transfer+" is not yet supported")
			continue
		}
		asset := strings.ToLower(requirement.Asset)
		if !addressPattern.MatchString(requirement.Asset) || !addressPattern.MatchString(requirement.PayTo) {
			reasons = append(reasons, "payment option contains an invalid address")
			continue
		}
		if wantedAsset != "" && asset != wantedAsset {
			continue
		}
		if allowedAsset := policy.AllowedAssets[network]; allowedAsset != "" && asset != strings.ToLower(allowedAsset) {
			reasons = append(reasons, "asset is not the configured USDC contract for "+network)
			continue
		}
		amount, ok := new(big.Int).SetString(requirement.Amount, 10)
		if !ok || amount.Sign() <= 0 {
			reasons = append(reasons, "payment amount is invalid")
			continue
		}
		if amount.Cmp(requestMax) > 0 || amount.Cmp(systemMax) > 0 {
			reasons = append(reasons, "payment amount exceeds the approved cap")
			continue
		}
		matches = append(matches, requirement)
	}
	if len(matches) == 0 {
		if len(reasons) == 0 {
			return PaymentRequirement{}, fmt.Errorf("no payment option matches the requested network or asset")
		}
		return PaymentRequirement{}, fmt.Errorf("no safe payment option: %s", strings.Join(uniqueStrings(reasons), "; "))
	}
	if len(matches) > 1 {
		return PaymentRequirement{}, fmt.Errorf("multiple safe payment options match; specify payment_network and payment_asset")
	}
	return matches[0], nil
}

type TypedDataSigner interface {
	Address(context.Context) (string, error)
	SignTypedData(context.Context, string, map[string]any) (string, error)
}

type RemoteSigner struct {
	baseURL string
	token   string
	client  *http.Client
	mu      sync.Mutex
	address string
}

func NewRemoteSigner(baseURL, token string) *RemoteSigner {
	return &RemoteSigner{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

func (s *RemoteSigner) Address(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.address != "" {
		return s.address, nil
	}
	var response struct {
		Keys []string `json:"keys"`
	}
	if err := s.call(ctx, http.MethodGet, "/api/v1/keys", nil, &response); err != nil {
		return "", err
	}
	if len(response.Keys) == 0 || !addressPattern.MatchString(response.Keys[0]) {
		return "", fmt.Errorf("remote signer returned no valid wallet address")
	}
	s.address = response.Keys[0]
	return s.address, nil
}

func (s *RemoteSigner) SignTypedData(ctx context.Context, address string, typedData map[string]any) (string, error) {
	if !addressPattern.MatchString(address) {
		return "", fmt.Errorf("invalid signer address")
	}
	var response struct {
		Signature string `json:"signature"`
	}
	path := "/api/v1/sign/" + url.PathEscape(address) + "/typed-data"
	if err := s.call(ctx, http.MethodPost, path, typedData, &response); err != nil {
		return "", err
	}
	if !signaturePattern.MatchString(response.Signature) {
		return "", fmt.Errorf("remote signer returned an invalid signature")
	}
	return response.Signature, nil
}

func (s *RemoteSigner) call(ctx context.Context, method, path string, payload, result any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode signer request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("create signer request: %w", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("remote signer unavailable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("remote signer returned HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(result); err != nil {
		return fmt.Errorf("decode remote signer response: %w", err)
	}
	return nil
}

type PaymentAuthorization struct {
	Header string
	Payer  string
}

type PaymentAuthorizer struct {
	signer TypedDataSigner
	policy PaymentPolicy
	max    *big.Int
}

func NewPaymentAuthorizer(signer TypedDataSigner, policy PaymentPolicy) (*PaymentAuthorizer, error) {
	maximum, ok := new(big.Int).SetString(policy.MaxAtomicAmount, 10)
	if !ok || maximum.Sign() <= 0 {
		return nil, fmt.Errorf("invalid operator payment cap")
	}
	return &PaymentAuthorizer{signer: signer, policy: policy, max: maximum}, nil
}

func (a *PaymentAuthorizer) Authorize(ctx context.Context, requirement PaymentRequirement, extensions map[string]any) (PaymentAuthorization, error) {
	network, chainID, err := normalizeNetwork(requirement.Network)
	if err != nil {
		return PaymentAuthorization{}, err
	}
	allowedChainID, allowed := a.policy.AllowedNetworks[network]
	if !allowed || allowedChainID != chainID {
		return PaymentAuthorization{}, fmt.Errorf("payment network is not allowed")
	}
	amount, ok := new(big.Int).SetString(requirement.Amount, 10)
	if !ok || amount.Sign() <= 0 || amount.Cmp(a.max) > 0 {
		return PaymentAuthorization{}, fmt.Errorf("payment amount exceeds the operator cap")
	}
	if requirement.Scheme != "exact" {
		return PaymentAuthorization{}, fmt.Errorf("unsupported payment scheme")
	}
	transfer, _ := requirement.Extra["assetTransferMethod"].(string)
	if transfer != "" && transfer != "eip3009" {
		return PaymentAuthorization{}, fmt.Errorf("unsupported asset transfer method %q", transfer)
	}
	if !addressPattern.MatchString(requirement.Asset) || !addressPattern.MatchString(requirement.PayTo) {
		return PaymentAuthorization{}, fmt.Errorf("invalid payment address")
	}
	if allowedAsset := a.policy.AllowedAssets[network]; allowedAsset != "" && !strings.EqualFold(allowedAsset, requirement.Asset) {
		return PaymentAuthorization{}, fmt.Errorf("payment asset is not allowed")
	}

	payer, err := a.signer.Address(ctx)
	if err != nil {
		return PaymentAuthorization{}, err
	}
	nonceBytes := make([]byte, 32)
	if _, err := rand.Read(nonceBytes); err != nil {
		return PaymentAuthorization{}, fmt.Errorf("generate payment nonce: %w", err)
	}
	nonce := "0x" + hex.EncodeToString(nonceBytes)
	validBefore := strconv.FormatInt(time.Now().Add(a.policy.AuthorizationTTL).Unix(), 10)
	domainName, domainVersion := eip3009Domain(requirement, network)
	typedData := map[string]any{
		"types": map[string]any{
			"EIP712Domain": []map[string]string{
				{"name": "name", "type": "string"},
				{"name": "version", "type": "string"},
				{"name": "chainId", "type": "uint256"},
				{"name": "verifyingContract", "type": "address"},
			},
			"TransferWithAuthorization": []map[string]string{
				{"name": "from", "type": "address"},
				{"name": "to", "type": "address"},
				{"name": "value", "type": "uint256"},
				{"name": "validAfter", "type": "uint256"},
				{"name": "validBefore", "type": "uint256"},
				{"name": "nonce", "type": "bytes32"},
			},
		},
		"primaryType": "TransferWithAuthorization",
		"domain": map[string]any{
			"name": domainName, "version": domainVersion, "chainId": chainID, "verifyingContract": requirement.Asset,
		},
		"message": map[string]any{
			"from": payer, "to": requirement.PayTo, "value": requirement.Amount,
			"validAfter": "0", "validBefore": validBefore, "nonce": nonce,
		},
	}
	signature, err := a.signer.SignTypedData(ctx, payer, typedData)
	if err != nil {
		return PaymentAuthorization{}, err
	}

	envelope := map[string]any{
		"x402Version": 2,
		"accepted": map[string]any{
			"scheme": requirement.Scheme, "network": requirement.Network, "amount": requirement.Amount,
			"asset": requirement.Asset, "payTo": requirement.PayTo,
			"maxTimeoutSeconds": requirement.MaxTimeoutSeconds, "extra": requirement.Extra,
		},
		"payload": map[string]any{
			"signature": signature,
			"authorization": map[string]any{
				"from": payer, "to": requirement.PayTo, "value": requirement.Amount,
				"validAfter": "0", "validBefore": validBefore, "nonce": nonce,
			},
		},
	}
	if echoed := echoExtensionInfo(extensions); len(echoed) > 0 {
		envelope["extensions"] = echoed
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return PaymentAuthorization{}, fmt.Errorf("encode payment authorization: %w", err)
	}
	return PaymentAuthorization{Header: base64.StdEncoding.EncodeToString(encoded), Payer: payer}, nil
}

func eip3009Domain(requirement PaymentRequirement, network string) (string, string) {
	if advertised, ok := requirement.Extra["eip712Domain"].(map[string]any); ok {
		name, nameOK := advertised["name"].(string)
		version, versionOK := advertised["version"].(string)
		if nameOK && versionOK && name != "" && version != "" {
			return name, version
		}
	}
	if network == "eip155:8453" {
		return "USD Coin", "2"
	}
	return "USDC", "2"
}

func echoExtensionInfo(extensions map[string]any) map[string]any {
	result := make(map[string]any)
	for name, raw := range extensions {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if info, exists := entry["info"]; exists {
			result[name] = map[string]any{"info": info}
		}
	}
	return result
}

func normalizeNetwork(value string) (string, int64, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "base", "eip155:8453", "8453":
		return "eip155:8453", 8453, nil
	case "base-sepolia", "eip155:84532", "84532":
		return "eip155:84532", 84532, nil
	default:
		return "", 0, fmt.Errorf("unsupported payment network %q", value)
	}
}

func atomicString(value any) (string, error) {
	switch typed := value.(type) {
	case string:
		if amount, ok := new(big.Int).SetString(typed, 10); ok && amount.Sign() >= 0 {
			return amount.String(), nil
		}
	case json.Number:
		if amount, ok := new(big.Int).SetString(typed.String(), 10); ok && amount.Sign() >= 0 {
			return amount.String(), nil
		}
	}
	return "", fmt.Errorf("not an atomic integer")
}

func integerValue(value any) (int, error) {
	text, err := atomicString(value)
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.Atoi(text)
	if err != nil {
		return 0, err
	}
	return parsed, nil
}

func stringValue(value any) string {
	result, _ := value.(string)
	return result
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{})
	var result []string
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
