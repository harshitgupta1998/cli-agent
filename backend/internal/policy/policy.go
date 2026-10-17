package policy

import (
	"errors"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/harsgupta/termind/backend/internal/models"
)

const (
	ExecutionDisabled                  = "disabled"
	ExecutionConfirmationRequired      = "confirmation_required"
	ExecutionRestrictedSafeAutocommand = "restricted_safe_autocommand"
)

var executablePattern = regexp.MustCompile(`^[A-Za-z0-9_.+-]+$`)

func ValidateWorkspacePermissions(update models.WorkspacePermissionsUpdate) error {
	switch update.ExecutionMode {
	case ExecutionDisabled, ExecutionConfirmationRequired, ExecutionRestrictedSafeAutocommand:
	default:
		return errors.New("execution_mode_invalid")
	}
	if update.MaxDurationSeconds < 1 || update.MaxDurationSeconds > 300 {
		return errors.New("max_duration_seconds_invalid")
	}
	if update.MaxOutputBytes < 1024 || update.MaxOutputBytes > 10*1024*1024 {
		return errors.New("max_output_bytes_invalid")
	}
	for _, root := range update.WritableRoots {
		clean := filepath.Clean(strings.TrimSpace(root))
		if root == "" || filepath.IsAbs(root) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return errors.New("writable_root_invalid")
		}
	}
	for _, command := range append(append([]string{}, update.AllowedCommands...), update.DeniedCommands...) {
		if !executablePattern.MatchString(command) || filepath.Base(command) != command {
			return errors.New("command_rule_invalid")
		}
	}
	return nil
}

func EvaluateWorkspaceCommand(permissions models.WorkspacePermissions, workspaceRoot string, cwd string, command string) (string, string) {
	if err := ValidateWorkspacePermissions(models.WorkspacePermissionsUpdate{
		ExecutionMode: permissions.ExecutionMode, WritableRoots: permissions.WritableRoots,
		AllowedCommands: permissions.AllowedCommands, DeniedCommands: permissions.DeniedCommands,
		MaxDurationSeconds: permissions.MaxDurationSeconds, MaxOutputBytes: permissions.MaxOutputBytes,
	}); err != nil {
		return "workspace_policy_invalid", "Workspace command policy is invalid; execution is denied."
	}
	if permissions.ExecutionMode == ExecutionDisabled {
		return "execution_disabled", "Command execution is disabled for this workspace."
	}
	root, err := filepath.EvalSymlinks(workspaceRoot)
	if err != nil {
		return "workspace_path_invalid", "Workspace path cannot be resolved."
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "workspace_path_invalid", "Workspace path cannot be resolved."
	}
	resolvedCWD, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return "working_directory_invalid", "Working directory cannot be resolved."
	}
	resolvedCWD, err = filepath.Abs(resolvedCWD)
	if err != nil || !within(root, resolvedCWD) {
		return "working_directory_denied", "Working directory is outside this workspace."
	}
	roots := permissions.WritableRoots
	if len(roots) == 0 {
		roots = []string{"."}
	}
	allowedRoot := false
	for _, relativeRoot := range roots {
		candidate := filepath.Join(root, relativeRoot)
		resolvedRoot, resolveErr := filepath.EvalSymlinks(candidate)
		if resolveErr != nil {
			continue
		}
		resolvedRoot, resolveErr = filepath.Abs(resolvedRoot)
		if resolveErr == nil && within(root, resolvedRoot) && within(resolvedRoot, resolvedCWD) {
			allowedRoot = true
			break
		}
	}
	if !allowedRoot {
		return "writable_root_denied", "Working directory is outside the workspace writable roots."
	}
	executable, simple := commandExecutable(command)
	if !simple {
		if permissions.ExecutionMode == ExecutionRestrictedSafeAutocommand || len(permissions.DeniedCommands) > 0 {
			return "shell_syntax_denied", "Shell operators and compound commands are not allowed by this workspace policy."
		}
		executable = ""
	}
	for _, denied := range permissions.DeniedCommands {
		if executable == denied {
			return "command_denied", "This executable is denied by workspace policy."
		}
	}
	if permissions.ExecutionMode == ExecutionRestrictedSafeAutocommand {
		if executable == "" || !contains(permissions.AllowedCommands, executable) {
			return "command_not_allowed", "This executable is not allowed by workspace policy."
		}
		if blocked, _ := BlockedCommand(command); blocked {
			return "command_denied", "Command blocked by Termind safety policy."
		}
	}
	return "", ""
}

func CanonicalWorkingDirectory(cwd string) (string, error) {
	resolved, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return "", err
	}
	return filepath.Abs(resolved)
}

func commandExecutable(command string) (string, bool) {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" || strings.ContainsAny(trimmed, ";|&><`$()\n\r") {
		return "", false
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return "", false
	}
	first := strings.Trim(fields[0], `"'`)
	if strings.Contains(first, string(filepath.Separator)) {
		return "", false
	}
	return first, true
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func within(root string, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

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
