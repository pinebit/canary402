package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"canary402/internal/canary"
)

var version = "dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg, err := canary.ConfigFromEnv()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	targetClient := canary.NewSafeHTTPClient(cfg.TargetPolicy)
	store, err := canary.NewFileStore(cfg.ReportDir)
	if err != nil {
		logger.Error("initialize report store", "error", err)
		os.Exit(1)
	}

	signer := canary.NewRemoteSigner(cfg.RemoteSignerURL, cfg.RemoteSignerToken)
	authorizer, err := canary.NewPaymentAuthorizer(signer, cfg.PaymentPolicy)
	if err != nil {
		logger.Error("initialize payment authorizer", "error", err)
		os.Exit(1)
	}

	var evaluator canary.SemanticEvaluator
	if cfg.LiteLLMToken != "" {
		evaluator = canary.NewLiteLLMEvaluator(cfg.LiteLLMBaseURL, cfg.LiteLLMToken, cfg.Model)
	}

	auditor := canary.NewAuditor(targetClient, authorizer, evaluator, store, cfg.Audit)
	handler := canary.NewHTTPHandler(auditor, store, canary.HTTPHandlerConfig{
		MaxConcurrent: cfg.MaxConcurrent,
		Version:       version,
		Logger:        logger,
	})

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}

	shutdownCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-shutdownCtx.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			logger.Error("graceful shutdown", "error", err)
		}
	}()

	logger.Info("canary402 starting", "address", server.Addr, "version", version, "semantic_evaluation", evaluator != nil)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
