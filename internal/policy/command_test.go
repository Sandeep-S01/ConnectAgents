package policy_test

import (
	"testing"

	"personal-ai-assistant/internal/policy"
)

func TestEvaluateCommandAllowsKnownSafeReadAndVerificationCommands(t *testing.T) {
	t.Parallel()

	commands := []string{
		"go test ./...",
		"go vet ./...",
		"go build ./cmd/gateway",
		"git status --short",
		"git diff",
		"git diff --stat",
	}

	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			t.Parallel()

			decision := policy.EvaluateCommand(command)

			if decision.Action != policy.ActionAllow {
				t.Fatalf("expected allow, got %q: %s", decision.Action, decision.Reason)
			}
		})
	}
}

func TestEvaluateCommandRequiresApprovalForSensitiveCommands(t *testing.T) {
	t.Parallel()

	commands := []string{
		"go get github.com/example/package",
		"npm install left-pad",
		"pip install requests",
		"dotnet add package Dapper",
		"git push origin main",
		"Remove-Item -Recurse .\\tmp",
		"rm -rf ./tmp",
		"go run ./cmd/gateway",
	}

	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			t.Parallel()

			decision := policy.EvaluateCommand(command)

			if decision.Action != policy.ActionRequireApproval {
				t.Fatalf("expected require_approval, got %q: %s", decision.Action, decision.Reason)
			}
		})
	}
}

func TestEvaluateCommandBlocksCredentialAndSystemAccess(t *testing.T) {
	t.Parallel()

	commands := []string{
		"type C:\\Users\\Sandeep\\.ssh\\id_rsa",
		"type C:\\Users\\Sandeep\\.ssh\\id_ed25519",
		"type C:\\Users\\Sandeep\\.aws\\credentials",
		"type .env",
		"type .npmrc",
		"type C:\\Users\\Sandeep\\.pypirc",
		"cat ~/.netrc",
		"cat ~/.kube/config",
		"cat ~/.config/gh/hosts.yml",
		"cat ~/.docker/config.json",
		"type C:\\Users\\Sandeep\\.netrc",
		"type C:\\Users\\Sandeep\\.kube\\config",
		"type C:\\Users\\Sandeep\\.config\\gh\\hosts.yml",
		"type C:\\Users\\Sandeep\\.docker\\config.json",
		"type C:\\Users\\Sandeep\\AppData\\Local\\Google\\Chrome\\User Data\\Default\\Login Data",
		"type C:\\Users\\Sandeep\\AppData\\Local\\Microsoft\\Edge\\User Data\\Default\\Login Data",
		"cat ~/Library/Application Support/Google/Chrome/Default/Login Data",
		"cat ~/.mozilla/firefox/profile/key4.db",
		"printenv",
		"printenv PAIA_AUTH_TOKEN",
		"env",
		"env | sort",
		"Get-ChildItem Env:",
		"echo $env:PAIA_AUTH_TOKEN",
		"Get-Content C:\\Users\\Sandeep\\AppData\\Roaming\\Microsoft\\Credentials\\secret",
		"cat /etc/passwd",
		"Remove-Item -Recurse C:\\Windows\\System32",
		"del C:\\Windows\\System32\\config\\SAM",
	}

	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			t.Parallel()

			decision := policy.EvaluateCommand(command)

			if decision.Action != policy.ActionBlock {
				t.Fatalf("expected block, got %q: %s", decision.Action, decision.Reason)
			}
		})
	}
}

func TestEvaluateCommandRequiresApprovalForUnknownCommand(t *testing.T) {
	t.Parallel()

	decision := policy.EvaluateCommand("python scripts/migrate.py")

	if decision.Action != policy.ActionRequireApproval {
		t.Fatalf("expected require_approval, got %q: %s", decision.Action, decision.Reason)
	}
}
