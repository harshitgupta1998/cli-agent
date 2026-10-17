package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/harsgupta/termind/backend/internal/models"
)

func TestValidateWorkspacePermissionsRejectsUnknownOrUnsafeValues(t *testing.T) {
	valid := models.WorkspacePermissionsUpdate{
		ExecutionMode: ExecutionConfirmationRequired, MaxDurationSeconds: 30, MaxOutputBytes: 1024,
	}
	if err := ValidateWorkspacePermissions(valid); err != nil {
		t.Fatalf("valid settings rejected: %v", err)
	}
	valid.ExecutionMode = "future-mode"
	if err := ValidateWorkspacePermissions(valid); err == nil {
		t.Fatal("unknown execution mode accepted")
	}
	valid.ExecutionMode = ExecutionConfirmationRequired
	valid.WritableRoots = []string{"../outside"}
	if err := ValidateWorkspacePermissions(valid); err == nil {
		t.Fatal("parent writable root accepted")
	}
}

func TestEvaluateWorkspaceCommandHonorsModeRulesAndWritableRoots(t *testing.T) {
	root := t.TempDir()
	allowed := filepath.Join(root, "src")
	denied := filepath.Join(root, "secrets")
	for _, dir := range []string{allowed, denied} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	permissions := models.WorkspacePermissions{
		ExecutionMode: ExecutionRestrictedSafeAutocommand,
		WritableRoots: []string{"src"}, AllowedCommands: []string{"go"}, DeniedCommands: []string{"git"},
		MaxDurationSeconds: 30, MaxOutputBytes: 1024,
	}
	if code, _ := EvaluateWorkspaceCommand(permissions, root, allowed, "go test ./..."); code != "" {
		t.Fatalf("allowed command denied: %s", code)
	}
	if code, _ := EvaluateWorkspaceCommand(permissions, root, denied, "go test"); code != "writable_root_denied" {
		t.Fatalf("secrets path decision = %q", code)
	}
	if code, _ := EvaluateWorkspaceCommand(permissions, root, allowed, "git status"); code != "command_denied" {
		t.Fatalf("denied executable decision = %q", code)
	}
	if code, _ := EvaluateWorkspaceCommand(permissions, root, allowed, "go test; echo unsafe"); code != "shell_syntax_denied" {
		t.Fatalf("compound command decision = %q", code)
	}
	outside := t.TempDir()
	link := filepath.Join(root, "external")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	permissions.WritableRoots = []string{"external"}
	if code, _ := EvaluateWorkspaceCommand(permissions, root, outside, "go test"); code != "working_directory_denied" {
		t.Fatalf("symlink escape decision = %q", code)
	}
}

func TestEvaluateRaisesDestructiveRisk(t *testing.T) {
	decision := Evaluate(models.CommandPlan{
		Command: "git reset --hard HEAD",
		Risk:    models.RiskSafe,
	})

	if decision.Risk != models.RiskDestructive {
		t.Fatalf("Risk = %q, want destructive", decision.Risk)
	}
	if !decision.RequiresConfirmation {
		t.Fatal("RequiresConfirmation = false, want true")
	}
	if len(decision.Warnings) == 0 {
		t.Fatal("expected destructive warning")
	}
}

func TestEvaluateRaisesPrivilegedRisk(t *testing.T) {
	decision := Evaluate(models.CommandPlan{
		Command: "sudo lsof -i :8000",
		Risk:    models.RiskSafe,
	})

	if decision.Risk != models.RiskPrivileged {
		t.Fatalf("Risk = %q, want privileged", decision.Risk)
	}
	if !decision.RequiresConfirmation {
		t.Fatal("RequiresConfirmation = false, want true")
	}
}

func TestEvaluatePreservesSafePlanWithoutConfirmation(t *testing.T) {
	decision := Evaluate(models.CommandPlan{
		Command: "pwd",
		Risk:    models.RiskSafe,
	})

	if decision.Risk != models.RiskSafe {
		t.Fatalf("Risk = %q, want safe", decision.Risk)
	}
	if decision.RequiresConfirmation {
		t.Fatal("RequiresConfirmation = true, want false")
	}
}

func TestBlockedCommandRejectsDangerousCommands(t *testing.T) {
	blocked, reason := BlockedCommand("rm -rf /tmp/example")

	if !blocked {
		t.Fatal("blocked = false, want true")
	}
	if !strings.Contains(strings.ToLower(reason), "blocked") {
		t.Fatalf("reason = %q, want blocked message", reason)
	}
}

func TestBlockedCommandAllowsReadOnlyCommands(t *testing.T) {
	blocked, reason := BlockedCommand("pwd")

	if blocked {
		t.Fatalf("blocked = true, reason = %q", reason)
	}
}
