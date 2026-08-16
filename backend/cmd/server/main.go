package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/harsgupta/termind/backend/internal/api"
	"github.com/harsgupta/termind/backend/internal/config"
	"github.com/harsgupta/termind/backend/internal/memory"
	"github.com/harsgupta/termind/backend/internal/planner"
	"github.com/harsgupta/termind/backend/internal/services"
)

func main() {
	cfg := config.Load()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var store memory.Store
	postgresStore, err := memory.NewPostgresStore(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Printf("Postgres unavailable, using in-memory mock search/record fallback: %v", err)
	}
	if postgresStore != nil {
		store = postgresStore
		defer postgresStore.Close()
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
	server := api.NewServer(cfg, agent)

	log.Printf("Termind API listening on %s", cfg.HTTPAddr)
	if err := http.ListenAndServe(cfg.HTTPAddr, server.Routes()); err != nil {
		log.Fatal(err)
	}
}
