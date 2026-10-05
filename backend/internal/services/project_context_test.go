package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectManifestCommandsFindsProjectEntrypoints(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/termind\n")
	writeFile(t, dir, "package.json", `{"scripts":{"dev":"vite","build":"vite build","typecheck":"tsc --noEmit"}}`)
	writeFile(t, dir, "Makefile", "test:\n\tgo test ./...\nbuild-cli:\n\tgo build ./cmd/termind\n")
	writeFile(t, dir, "requirements-dev.txt", "pytest==9.0.2\n")
	writeFile(t, dir, "docker-compose.yml", "services: {}\n")

	commands := detectManifestCommands(dir)
	byCommand := map[string]string{}
	for _, command := range commands {
		byCommand[command.Command] = command.Source
	}

	assertSource(t, byCommand, "go test ./...", "go.mod")
	assertSource(t, byCommand, "go run ./cmd/server", "go.mod")
	assertSource(t, byCommand, "npm run dev", "package.json")
	assertSource(t, byCommand, "npm run typecheck", "package.json")
	assertSource(t, byCommand, "npm run build", "package.json")
	assertSource(t, byCommand, "docker compose up -d --build", "docker-compose.yml")
	assertSource(t, byCommand, "pytest", "requirements-dev.txt")
	assertSource(t, byCommand, "make build-cli", "Makefile")
}

func writeFile(t *testing.T, dir string, name string, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertSource(t *testing.T, commands map[string]string, command string, source string) {
	t.Helper()
	got, ok := commands[command]
	if !ok {
		t.Fatalf("command %q was not detected", command)
	}
	if got != source {
		t.Fatalf("command %q source = %q, want %q", command, got, source)
	}
}
