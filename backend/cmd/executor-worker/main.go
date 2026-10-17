package main

import (
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/harsgupta/termind/backend/internal/config"
	"github.com/harsgupta/termind/backend/internal/executor"
)

func main() {
	cfg := config.Load()
	if err := os.MkdirAll(filepath.Dir(cfg.ExecutorSocketPath), 0o700); err != nil {
		log.Fatalf("create executor socket directory: %v", err)
	}
	if info, err := os.Lstat(cfg.ExecutorSocketPath); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			log.Fatalf("executor socket path exists and is not a socket: %s", cfg.ExecutorSocketPath)
		}
		if err := os.Remove(cfg.ExecutorSocketPath); err != nil {
			log.Fatalf("remove stale executor socket: %v", err)
		}
	} else if !os.IsNotExist(err) {
		log.Fatalf("inspect executor socket path: %v", err)
	}

	listener, err := net.Listen("unix", cfg.ExecutorSocketPath)
	if err != nil {
		log.Fatalf("listen on executor socket: %v", err)
	}
	defer os.Remove(cfg.ExecutorSocketPath)
	if err := os.Chmod(cfg.ExecutorSocketPath, 0o600); err != nil {
		log.Fatalf("secure executor socket: %v", err)
	}

	runner := executor.NewRunner(
		cfg.CommandTimeout,
		cfg.ExecutionAllowedRoots,
		cfg.CommandMaxOutputBytes,
		cfg.ExecutionEnvAllowlist,
	)
	server := &http.Server{
		Handler:           executor.NewWorkerHandler(runner),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("Termind executor worker listening on a private Unix socket")
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		log.Fatalf("executor worker stopped: %v", err)
	}
}
