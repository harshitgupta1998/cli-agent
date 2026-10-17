package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/harsgupta/termind/backend/internal/api"
	"github.com/harsgupta/termind/backend/internal/auth"
	"github.com/harsgupta/termind/backend/internal/config"
	"github.com/harsgupta/termind/backend/internal/embeddings"
	"github.com/harsgupta/termind/backend/internal/executor"
	"github.com/harsgupta/termind/backend/internal/memory"
	"github.com/harsgupta/termind/backend/internal/planner"
	"github.com/harsgupta/termind/backend/internal/services"
	"github.com/harsgupta/termind/backend/internal/voice"
)

func main() {
	cfg := config.Load()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var store memory.Store
	var authRepository auth.Repository
	embedder := embeddings.NewOllamaEmbedder(cfg.OllamaBaseURL, cfg.OllamaEmbedModel)
	postgresStore, err := memory.NewPostgresStore(ctx, cfg.DatabaseURL, embedder)
	if err != nil {
		if cfg.LocalOnlyMode != "true" {
			log.Fatalf("authenticated mode requires Postgres and auth migrations: %v", err)
		}
		log.Printf("Postgres unavailable, using in-memory mock search/record fallback: %v", err)
	}
	if postgresStore != nil {
		store = postgresStore
		authRepository = postgresStore
		defer postgresStore.Close()
	}
	if cfg.LocalOnlyMode != "true" && len(cfg.BootstrapToken) < 32 {
		log.Fatal("authenticated mode requires TERMIND_BOOTSTRAP_TOKEN with at least 32 characters")
	}
	if cfg.LocalOnlyMode != "true" && len(cfg.CommandApprovalSecret) < 32 {
		log.Fatal("authenticated mode requires COMMAND_APPROVAL_SECRET with at least 32 characters")
	}
	principal := auth.Principal{UserID: cfg.BootstrapUserID, Name: cfg.BootstrapUserName}
	authManager := auth.NewManager(authRepository, cfg.BootstrapToken, principal)
	if err := authManager.EnsureBootstrapUser(ctx); err != nil {
		log.Fatalf("initialize bootstrap identity: %v", err)
	}

	rulePlanner := planner.NewRulePlanner()
	var commandPlanner planner.Planner = rulePlanner
	if cfg.PlannerMode == "ollama" {
		commandPlanner = planner.NewFallbackPlanner(
			planner.NewOllamaPlanner(cfg.OllamaBaseURL, cfg.OllamaModel),
			rulePlanner,
		)
	}

	agent := services.NewAgentService(store, commandPlanner, cfg.CommandTimeout)
	agent = agent.WithExecutionControls(cfg.ExecutionAllowedRoots, cfg.CommandMaxOutputBytes, cfg.ExecutionEnvAllowlist)
	agent = agent.WithExecutor(executor.NewUnixClient(cfg.ExecutorSocketPath))
	agent = agent.WithVoiceConfig(cfg.VoiceEnabled == "true", cfg.VoiceSTTProvider, cfg.VoiceMaxSeconds)
	agent = agent.WithVoiceMaxBytes(cfg.VoiceMaxBytes)
	if cfg.VoiceSTTProvider == "command" {
		agent = agent.WithVoiceTranscriber(voice.NewCommandTranscriber(cfg.VoiceSTTCommand))
	}
	server := api.NewServer(cfg, agent, authManager)

	log.Printf("Termind API listening on %s", cfg.HTTPAddr)
	if err := http.ListenAndServe(cfg.HTTPAddr, server.Routes()); err != nil {
		log.Fatal(err)
	}
}
