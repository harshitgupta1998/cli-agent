package policy

import (
	"strings"
	"testing"

	"github.com/harsgupta/termind/backend/internal/models"
)

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
