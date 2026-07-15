package canary

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestParsePaymentChallenge(t *testing.T) {
	t.Parallel()
	challenge, err := ParsePaymentChallenge([]byte(`{
      "x402Version": 2,
      "accepts": [{
        "scheme": "exact",
        "network": "eip155:84532",
        "amount": "1000",
        "asset": "0x036CbD53842c5426634e7929541eC2318f3dCF7e",
        "payTo": "0x1111111111111111111111111111111111111111",
        "maxTimeoutSeconds": 300,
        "extra": {"assetTransferMethod": "eip3009"}
      }]
    }`))
	if err != nil {
		t.Fatal(err)
	}
	if challenge.Version != 2 || len(challenge.Accepts) != 1 || challenge.Accepts[0].Amount != "1000" {
		t.Fatalf("unexpected challenge: %+v", challenge)
	}
}

func TestParsePaymentChallengeResponseHeader(t *testing.T) {
	t.Parallel()
	payload := []byte(`{"x402Version":2,"accepts":[{"scheme":"exact","network":"eip155:84532","amount":"1000","asset":"0x036CbD53842c5426634e7929541eC2318f3dCF7e","payTo":"0x1111111111111111111111111111111111111111"}]}`)
	headers := http.Header{"Payment-Required": []string{base64.StdEncoding.EncodeToString(payload)}}
	challenge, transport, authorizationHeader, err := ParsePaymentChallengeResponse(headers, nil)
	if err != nil {
		t.Fatal(err)
	}
	if challenge.Version != 2 || transport != "PAYMENT-REQUIRED" || authorizationHeader != "PAYMENT-SIGNATURE" {
		t.Fatalf("unexpected header challenge: %+v %s %s", challenge, transport, authorizationHeader)
	}
}

func TestSelectPaymentFailsClosed(t *testing.T) {
	t.Parallel()
	policy := PaymentPolicy{
		MaxAtomicAmount: "10000",
		AllowedNetworks: map[string]int64{"eip155:84532": 84532},
		AllowedAssets:   map[string]string{"eip155:84532": strings.ToLower(baseSepoliaUSDC)},
	}
	requirement := PaymentRequirement{
		Scheme: "exact", Network: "base-sepolia", Amount: "1001", Asset: baseSepoliaUSDC,
		PayTo: "0x1111111111111111111111111111111111111111", Extra: map[string]any{"assetTransferMethod": "eip3009"},
	}
	if _, err := SelectPayment([]PaymentRequirement{requirement}, AuditRequest{Pay: true, MaxPaymentAtomic: "1000"}, policy); err == nil {
		t.Fatal("expected over-budget payment to fail")
	}
	requirement.Amount = "1000"
	requirement.Extra["assetTransferMethod"] = "permit2"
	if _, err := SelectPayment([]PaymentRequirement{requirement}, AuditRequest{Pay: true, MaxPaymentAtomic: "1000"}, policy); err == nil {
		t.Fatal("expected unsupported Permit2 payment to fail")
	}
}

func TestPaymentAuthorizerCreatesBoundedEnvelope(t *testing.T) {
	t.Parallel()
	signer := &fakeSigner{
		address:   "0x2222222222222222222222222222222222222222",
		signature: "0x" + strings.Repeat("cd", 65),
	}
	policy := PaymentPolicy{
		MaxAtomicAmount:  "10000",
		AllowedNetworks:  map[string]int64{"eip155:84532": 84532},
		AllowedAssets:    map[string]string{"eip155:84532": strings.ToLower(baseSepoliaUSDC)},
		AuthorizationTTL: 30 * time.Minute,
	}
	authorizer, err := NewPaymentAuthorizer(signer, policy)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := authorizer.Authorize(context.Background(), PaymentRequirement{
		Scheme: "exact", Network: "eip155:84532", Amount: "1000", Asset: baseSepoliaUSDC,
		PayTo: "0x1111111111111111111111111111111111111111", MaxTimeoutSeconds: 300,
		Extra: map[string]any{"assetTransferMethod": "eip3009"},
	}, map[string]any{
		"bazaar": map[string]any{"info": map[string]any{"listed": true}, "schema": map[string]any{"must_not_echo": true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := base64.StdEncoding.DecodeString(auth.Header)
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		t.Fatal(err)
	}
	accepted := envelope["accepted"].(map[string]any)
	if accepted["amount"] != "1000" || accepted["network"] != "eip155:84532" {
		t.Fatalf("authorization changed quoted terms: %#v", accepted)
	}
	extensions := envelope["extensions"].(map[string]any)
	bazaar := extensions["bazaar"].(map[string]any)
	if _, exists := bazaar["schema"]; exists {
		t.Fatal("server extension schema must not be echoed")
	}
}
