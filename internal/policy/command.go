package policy

import "strings"

type Action string

const (
	ActionAllow           Action = "allow"
	ActionRequireApproval Action = "require_approval"
	ActionBlock           Action = "block"
)

type Decision struct {
	Action  Action `json:"action"`
	Reason  string `json:"reason"`
	Command string `json:"command"`
	Kind    string `json:"kind"`
}

func EvaluateCommand(command string) Decision {
	normalized := normalize(command)
	lower := strings.ToLower(normalized)

	if normalized == "" {
		return decision(command, ActionBlock, "empty_command", "empty commands are not allowed")
	}
	if containsBlockedCredentialAccess(lower) {
		return decision(command, ActionBlock, "blocked_path", "credential and system paths are blocked")
	}
	if isSafeCommand(lower) {
		return decision(command, ActionAllow, "safe_command", "known read or verification command")
	}
	if requiresApproval(lower) {
		return decision(command, ActionRequireApproval, "sensitive_command", "command changes dependencies, files, git remotes, or starts a process")
	}
	return decision(command, ActionRequireApproval, "unknown_command", "unknown commands require explicit approval")
}

func decision(command string, action Action, kind string, reason string) Decision {
	return Decision{
		Action:  action,
		Reason:  reason,
		Command: command,
		Kind:    kind,
	}
}

func normalize(command string) string {
	return strings.Join(strings.Fields(command), " ")
}

func isSafeCommand(command string) bool {
	safeCommands := map[string]struct{}{
		"go test ./...":             {},
		"go vet ./...":              {},
		"go build ./cmd/gateway":    {},
		"git status":                {},
		"git status --short":        {},
		"git diff":                  {},
		"git diff --stat":           {},
		"git branch --show-current": {},
	}
	_, ok := safeCommands[command]
	return ok
}

func requiresApproval(command string) bool {
	approvalPrefixes := []string{
		"go get ",
		"go install ",
		"go run ",
		"npm install",
		"npm i ",
		"pnpm install",
		"yarn add ",
		"pip install ",
		"dotnet add package ",
		"git push",
		"git commit",
		"git tag",
		"git reset",
		"git clean",
		"rm ",
		"del ",
		"remove-item ",
		"rmdir ",
		"deploy",
		"publish",
	}
	for _, prefix := range approvalPrefixes {
		if strings.HasPrefix(command, prefix) {
			return true
		}
	}
	return strings.Contains(command, " -rf ") || strings.Contains(command, " --force")
}

func containsBlockedCredentialAccess(command string) bool {
	if command == "env" || strings.HasPrefix(command, "env ") ||
		command == "printenv" || strings.HasPrefix(command, "printenv ") ||
		strings.Contains(command, " env:") || strings.Contains(command, "$env:") {
		return true
	}

	blockedFragments := []string{
		".ssh\\id_rsa",
		".ssh/id_rsa",
		".ssh\\id_ed25519",
		".ssh/id_ed25519",
		".aws\\credentials",
		".aws/credentials",
		".config\\gh\\hosts.yml",
		".config/gh/hosts.yml",
		".docker\\config.json",
		".docker/config.json",
		".env",
		".npmrc",
		".pypirc",
		".netrc",
		".kube\\config",
		".kube/config",
		"google\\chrome\\user data\\default\\login data",
		"google/chrome/default/login data",
		"microsoft\\edge\\user data\\default\\login data",
		"mozilla/firefox",
		"key4.db",
		"appdata\\roaming\\microsoft\\credentials",
		"windows\\system32",
		"system32\\config\\sam",
		"/etc/passwd",
		"/etc/shadow",
		"browser password",
	}
	for _, fragment := range blockedFragments {
		if strings.Contains(command, fragment) {
			return true
		}
	}
	return false
}
