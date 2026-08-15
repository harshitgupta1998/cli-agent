package main

import (
	"log"
	"net/http"

	"github.com/harsgupta/termind/backend/internal/api"
	"github.com/harsgupta/termind/backend/internal/config"
	"github.com/harsgupta/termind/backend/internal/services"
)

func main() {
	cfg := config.Load()
	agent := services.NewMockAgent()
	server := api.NewServer(cfg, agent)

	log.Printf("Termind API listening on %s", cfg.HTTPAddr)
	if err := http.ListenAndServe(cfg.HTTPAddr, server.Routes()); err != nil {
		log.Fatal(err)
	}
}
