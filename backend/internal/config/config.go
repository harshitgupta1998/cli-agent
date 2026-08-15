package config

import "os"

type Config struct {
	HTTPAddr      string
	DatabaseURL   string
	OllamaBaseURL string
	LocalOnlyMode string
	CORSOrigin    string
}

func Load() Config {
	return Config{
		HTTPAddr:      env("HTTP_ADDR", ":8000"),
		DatabaseURL:   env("DATABASE_URL", "postgres://termind:termind@database:5432/termind?sslmode=disable"),
		OllamaBaseURL: env("OLLAMA_BASE_URL", "http://host.docker.internal:11434"),
		LocalOnlyMode: env("LOCAL_ONLY_MODE", "true"),
		CORSOrigin:    env("CORS_ORIGIN", "http://localhost:5173"),
	}
}

func env(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
