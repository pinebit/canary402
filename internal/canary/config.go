package canary

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	baseUSDC        = "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"
	baseSepoliaUSDC = "0x036CbD53842c5426634e7929541eC2318f3dCF7e"
)

type Config struct {
	Port              string
	ReportDir         string
	RemoteSignerURL   string
	RemoteSignerToken string
	LiteLLMBaseURL    string
	LiteLLMToken      string
	Model             string
	MaxConcurrent     int
	TargetPolicy      TargetPolicy
	PaymentPolicy     PaymentPolicy
	Audit             AuditConfig
}

type TargetPolicy struct {
	AllowHTTP           bool
	AllowPrivateTargets bool
	Timeout             time.Duration
	MaxResponseBytes    int64
}

type PaymentPolicy struct {
	MaxAtomicAmount  string
	AllowedNetworks  map[string]int64
	AllowedAssets    map[string]string
	AuthorizationTTL time.Duration
}

type AuditConfig struct {
	MaxRequestBodyBytes int64
	MaxExpectationBytes int
	SemanticInputBytes  int
}

func ConfigFromEnv() (Config, error) {
	maxConcurrent, err := envInt("CANARY_MAX_CONCURRENT", 4)
	if err != nil || maxConcurrent < 1 || maxConcurrent > 64 {
		return Config{}, fmt.Errorf("CANARY_MAX_CONCURRENT must be between 1 and 64")
	}
	timeoutSeconds, err := envInt("CANARY_TARGET_TIMEOUT_SECONDS", 20)
	if err != nil || timeoutSeconds < 1 || timeoutSeconds > 120 {
		return Config{}, fmt.Errorf("CANARY_TARGET_TIMEOUT_SECONDS must be between 1 and 120")
	}

	allowedNetworks := map[string]int64{
		"eip155:8453":  8453,
		"eip155:84532": 84532,
	}
	if raw := strings.TrimSpace(os.Getenv("CANARY_ALLOWED_NETWORKS")); raw != "" {
		allowedNetworks = make(map[string]int64)
		for _, value := range strings.Split(raw, ",") {
			network, chainID, err := normalizeNetwork(strings.TrimSpace(value))
			if err != nil {
				return Config{}, fmt.Errorf("CANARY_ALLOWED_NETWORKS: %w", err)
			}
			allowedNetworks[network] = chainID
		}
	}

	allowedAssets := map[string]string{
		"eip155:8453":  strings.ToLower(baseUSDC),
		"eip155:84532": strings.ToLower(baseSepoliaUSDC),
	}

	return Config{
		Port:              envString("PORT", "8080"),
		ReportDir:         envString("CANARY_REPORT_DIR", "/data/reports"),
		RemoteSignerURL:   envString("REMOTE_SIGNER_URL", "http://remote-signer.hermes-obol-agent.svc.cluster.local:9000"),
		RemoteSignerToken: strings.TrimSpace(os.Getenv("REMOTE_SIGNER_TOKEN")),
		LiteLLMBaseURL:    envString("LITELLM_BASE_URL", "http://litellm.llm.svc.cluster.local:4000"),
		LiteLLMToken:      strings.TrimSpace(os.Getenv("LITELLM_MASTER_KEY")),
		Model:             envString("CANARY_MODEL", "openrouter/auto"),
		MaxConcurrent:     maxConcurrent,
		TargetPolicy: TargetPolicy{
			AllowHTTP:           envBool("CANARY_ALLOW_HTTP", false),
			AllowPrivateTargets: envBool("CANARY_ALLOW_PRIVATE_TARGETS", false),
			Timeout:             time.Duration(timeoutSeconds) * time.Second,
			MaxResponseBytes:    1 << 20,
		},
		PaymentPolicy: PaymentPolicy{
			MaxAtomicAmount:  envString("CANARY_MAX_PAYMENT_ATOMIC", "20000"),
			AllowedNetworks:  allowedNetworks,
			AllowedAssets:    allowedAssets,
			AuthorizationTTL: 30 * time.Minute,
		},
		Audit: AuditConfig{
			MaxRequestBodyBytes: 64 << 10,
			MaxExpectationBytes: 2_000,
			SemanticInputBytes:  12_000,
		},
	}, nil
}

func envString(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envBool(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt(name string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	return strconv.Atoi(value)
}
