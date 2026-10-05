package policy

import (
	"strings"

	"github.com/harsgupta/termind/backend/internal/models"
)

func Evaluate(plan models.CommandPlan) models.PolicyDecision {
	command := strings.ToLower(plan.Command)
	risk := plan.Risk
	warnings := []string{}

	for _, marker := range []string{"rm -rf", "git reset --hard", "git clean", "docker system prune"} {
		if strings.Contains(command, marker) {
			risk = models.RiskDestructive
			warnings = append(warnings, "This command can permanently remove or reset data.")
			break
		}
	}

	for _, marker := range []string{"sudo", "chown -r", "chmod -r"} {
		if strings.Contains(command, marker) {
			risk = models.RiskPrivileged
			warnings = append(warnings, "This command requests elevated or broad system permissions.")
			break
		}
	}

	return models.PolicyDecision{
		Risk:                 risk,
		RequiresConfirmation: plan.RequiresConfirmation || risk != models.RiskSafe,
		Warnings:             warnings,
	}
}

func BlockedCommand(command string) (bool, string) {
	normalized := strings.ToLower(strings.TrimSpace(command))
	if normalized == "" {
		return true, "Empty commands cannot be executed."
	}

	blockedMarkers := []string{
		"rm -rf",
		"git reset --hard",
		"git clean",
		"docker system prune",
		"mkfs",
		":(){",
		"dd if=",
		"drop table",
		"shutdown",
		"reboot",
	}
	for _, marker := range blockedMarkers {
		if strings.Contains(normalized, marker) {
			return true, "Command blocked by Termind safety policy."
		}
	}
	if strings.Contains(normalized, "sudo ") {
		return true, "Privileged commands are blocked in backend execution for now."
	}

	return false, ""
}
