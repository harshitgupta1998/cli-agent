package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr         string
	DatabaseURL      string
	OllamaBaseURL    string
	OllamaModel      string
	OllamaEmbedModel string
	PlannerMode      string
	CommandTimeout   time.Duration
	LocalOnlyMode    string
	CORSOrigin       string
}

func Load() Config {
	return Config{
		HTTPAddr:         env("HTTP_ADDR", ":8000"),
		DatabaseURL:      env("DATABASE_URL", "postgres://termind:termind@database:5432/termind?sslmode=disable"),
		OllamaBaseURL:    env("OLLAMA_BASE_URL", "http://host.docker.internal:11434"),
		OllamaModel:      env("OLLAMA_MODEL", "llama3.2:3b"),
		OllamaEmbedModel: env("OLLAMA_EMBED_MODEL", "nomic-embed-text"),
		PlannerMode:      env("PLANNER_MODE", "ollama"),
		CommandTimeout:   secondsEnv("COMMAND_TIMEOUT_SECONDS", 30),
		LocalOnlyMode:    env("LOCAL_ONLY_MODE", "true"),
		CORSOrigin:       env("CORS_ORIGIN", "http://localhost:5173"),
	}
}

func secondsEnv(key string, fallback int) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return time.Duration(fallback) * time.Second
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		return time.Duration(fallback) * time.Second
	}
	return time.Duration(seconds) * time.Second
}

func env(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
