package config_test

import (
	"testing"

	"personal-ai-assistant/internal/config"
)

func TestLoadUsesSafeLocalDefaults(t *testing.T) {
	t.Setenv("PAIA_ENV", "")
	t.Setenv("PAIA_ADDR", "")
	t.Setenv("PAIA_DB_PATH", "")
	t.Setenv("PAIA_AUTH_TOKEN", "")
	t.Setenv("PAIA_LOG_PATH", "")
	t.Setenv("PAIA_LOG_MAX_BYTES", "")
	t.Setenv("PAIA_TASK_TIMEOUT_SECONDS", "")
	t.Setenv("PAIA_LOGIN_FAILURE_LIMIT", "")
	t.Setenv("PAIA_LOGIN_FAILURE_WINDOW_SECONDS", "")
	t.Setenv("PAIA_API_WRITE_LIMIT", "")
	t.Setenv("PAIA_API_WRITE_RATE_WINDOW_SECONDS", "")
	t.Setenv("PAIA_TLS_CERT_PATH", "")
	t.Setenv("PAIA_TLS_KEY_PATH", "")

	cfg := config.Load()

	if cfg.Env != "" {
		t.Fatalf("expected empty environment by default")
	}
	if cfg.Addr != "127.0.0.1:8080" {
		t.Fatalf("expected localhost default address, got %q", cfg.Addr)
	}
	if cfg.DBPath != "data/gateway.db" {
		t.Fatalf("expected default db path, got %q", cfg.DBPath)
	}
	if cfg.AuthToken != "" {
		t.Fatalf("expected empty auth token by default")
	}
	if cfg.LogPath != "" {
		t.Fatalf("expected empty log path by default")
	}
	if cfg.LogMaxBytes != 10*1024*1024 {
		t.Fatalf("expected 10 MiB log max bytes, got %d", cfg.LogMaxBytes)
	}
	if cfg.TaskTimeoutSeconds != 2*60*60 {
		t.Fatalf("expected 2 hour task timeout, got %d seconds", cfg.TaskTimeoutSeconds)
	}
	if cfg.LoginFailureLimit != 5 {
		t.Fatalf("expected login failure limit 5, got %d", cfg.LoginFailureLimit)
	}
	if cfg.LoginFailureWindowSeconds != 5*60 {
		t.Fatalf("expected 5 minute login failure window, got %d seconds", cfg.LoginFailureWindowSeconds)
	}
	if cfg.APIWriteLimit != 120 {
		t.Fatalf("expected API write limit 120, got %d", cfg.APIWriteLimit)
	}
	if cfg.APIWriteRateWindowSeconds != 60 {
		t.Fatalf("expected API write rate window 60 seconds, got %d", cfg.APIWriteRateWindowSeconds)
	}
	if cfg.TLSCertPath != "" {
		t.Fatalf("expected empty TLS cert path by default")
	}
	if cfg.TLSKeyPath != "" {
		t.Fatalf("expected empty TLS key path by default")
	}
}

func TestLoadUsesEnvironmentOverrides(t *testing.T) {
	t.Setenv("PAIA_ENV", "production")
	t.Setenv("PAIA_ADDR", "127.0.0.1:9090")
	t.Setenv("PAIA_DB_PATH", "tmp/test.db")
	t.Setenv("PAIA_AUTH_TOKEN", "secret-token")
	t.Setenv("PAIA_LOG_PATH", "logs/gateway.log")
	t.Setenv("PAIA_LOG_MAX_BYTES", "2048")
	t.Setenv("PAIA_TASK_TIMEOUT_SECONDS", "60")
	t.Setenv("PAIA_LOGIN_FAILURE_LIMIT", "3")
	t.Setenv("PAIA_LOGIN_FAILURE_WINDOW_SECONDS", "120")
	t.Setenv("PAIA_API_WRITE_LIMIT", "10")
	t.Setenv("PAIA_API_WRITE_RATE_WINDOW_SECONDS", "30")
	t.Setenv("PAIA_TLS_CERT_PATH", "certs/gateway.crt")
	t.Setenv("PAIA_TLS_KEY_PATH", "certs/gateway.key")

	cfg := config.Load()

	if cfg.Env != "production" {
		t.Fatalf("expected overridden environment")
	}
	if cfg.Addr != "127.0.0.1:9090" {
		t.Fatalf("expected overridden address, got %q", cfg.Addr)
	}
	if cfg.DBPath != "tmp/test.db" {
		t.Fatalf("expected overridden db path, got %q", cfg.DBPath)
	}
	if cfg.AuthToken != "secret-token" {
		t.Fatalf("expected overridden auth token")
	}
	if cfg.LogPath != "logs/gateway.log" {
		t.Fatalf("expected overridden log path")
	}
	if cfg.LogMaxBytes != 2048 {
		t.Fatalf("expected overridden log max bytes, got %d", cfg.LogMaxBytes)
	}
	if cfg.TaskTimeoutSeconds != 60 {
		t.Fatalf("expected overridden task timeout seconds, got %d", cfg.TaskTimeoutSeconds)
	}
	if cfg.LoginFailureLimit != 3 {
		t.Fatalf("expected overridden login failure limit, got %d", cfg.LoginFailureLimit)
	}
	if cfg.LoginFailureWindowSeconds != 120 {
		t.Fatalf("expected overridden login failure window, got %d", cfg.LoginFailureWindowSeconds)
	}
	if cfg.APIWriteLimit != 10 {
		t.Fatalf("expected overridden API write limit, got %d", cfg.APIWriteLimit)
	}
	if cfg.APIWriteRateWindowSeconds != 30 {
		t.Fatalf("expected overridden API write rate window, got %d", cfg.APIWriteRateWindowSeconds)
	}
	if cfg.TLSCertPath != "certs/gateway.crt" {
		t.Fatalf("expected overridden TLS cert path")
	}
	if cfg.TLSKeyPath != "certs/gateway.key" {
		t.Fatalf("expected overridden TLS key path")
	}
}

func TestLoadRejectsMalformedNumericEnvironmentValues(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{name: "log max bytes", key: "PAIA_LOG_MAX_BYTES"},
		{name: "task timeout", key: "PAIA_TASK_TIMEOUT_SECONDS"},
		{name: "login failure limit", key: "PAIA_LOGIN_FAILURE_LIMIT"},
		{name: "login failure window", key: "PAIA_LOGIN_FAILURE_WINDOW_SECONDS"},
		{name: "API write limit", key: "PAIA_API_WRITE_LIMIT"},
		{name: "API write rate window", key: "PAIA_API_WRITE_RATE_WINDOW_SECONDS"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PAIA_ADDR", "127.0.0.1:8080")
			t.Setenv("PAIA_DB_PATH", "")
			t.Setenv("PAIA_AUTH_TOKEN", "")
			t.Setenv("PAIA_LOG_PATH", "")
			t.Setenv("PAIA_LOG_MAX_BYTES", "")
			t.Setenv("PAIA_TASK_TIMEOUT_SECONDS", "")
			t.Setenv("PAIA_LOGIN_FAILURE_LIMIT", "")
			t.Setenv("PAIA_LOGIN_FAILURE_WINDOW_SECONDS", "")
			t.Setenv("PAIA_API_WRITE_LIMIT", "")
			t.Setenv("PAIA_API_WRITE_RATE_WINDOW_SECONDS", "")
			t.Setenv("PAIA_TLS_CERT_PATH", "")
			t.Setenv("PAIA_TLS_KEY_PATH", "")
			t.Setenv(tt.key, "not-a-number")

			cfg := config.Load()
			if err := cfg.Validate(); err == nil {
				t.Fatalf("expected malformed %s to be rejected", tt.key)
			}
		})
	}
}

func TestValidateAllowsLocalAddressWithoutAuthToken(t *testing.T) {
	cfg := config.Config{
		Addr:                      "127.0.0.1:8080",
		LogMaxBytes:               10 * 1024 * 1024,
		TaskTimeoutSeconds:        2 * 60 * 60,
		LoginFailureLimit:         5,
		LoginFailureWindowSeconds: 5 * 60,
		APIWriteLimit:             120,
		APIWriteRateWindowSeconds: 60,
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected local address without auth token to be valid: %v", err)
	}
}

func TestValidateRejectsUnknownEnvironment(t *testing.T) {
	cfg := config.Config{
		Env:                       "prod",
		Addr:                      "127.0.0.1:8080",
		LogMaxBytes:               10 * 1024 * 1024,
		TaskTimeoutSeconds:        2 * 60 * 60,
		LoginFailureLimit:         5,
		LoginFailureWindowSeconds: 5 * 60,
		APIWriteLimit:             120,
		APIWriteRateWindowSeconds: 60,
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected unknown environment to be rejected")
	}
}

func TestValidateAllowsDevelopmentEnvironment(t *testing.T) {
	cfg := config.Config{
		Env:                       "development",
		Addr:                      "127.0.0.1:8080",
		LogMaxBytes:               10 * 1024 * 1024,
		TaskTimeoutSeconds:        2 * 60 * 60,
		LoginFailureLimit:         5,
		LoginFailureWindowSeconds: 5 * 60,
		APIWriteLimit:             120,
		APIWriteRateWindowSeconds: 60,
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected development environment to be valid: %v", err)
	}
}

func TestValidateRejectsProductionModeWithoutAuthToken(t *testing.T) {
	cfg := config.Config{
		Env:                       "production",
		Addr:                      "127.0.0.1:8080",
		LogMaxBytes:               10 * 1024 * 1024,
		TaskTimeoutSeconds:        2 * 60 * 60,
		LoginFailureLimit:         5,
		LoginFailureWindowSeconds: 5 * 60,
		APIWriteLimit:             120,
		APIWriteRateWindowSeconds: 60,
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected production mode without auth token to be rejected")
	}
}

func TestValidateRejectsProductionModeWithWeakAuthToken(t *testing.T) {
	cfg := config.Config{
		Env:                       "production",
		Addr:                      "127.0.0.1:8080",
		AuthToken:                 "short-token",
		LogMaxBytes:               10 * 1024 * 1024,
		TaskTimeoutSeconds:        2 * 60 * 60,
		LoginFailureLimit:         5,
		LoginFailureWindowSeconds: 5 * 60,
		APIWriteLimit:             120,
		APIWriteRateWindowSeconds: 60,
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected production mode with weak auth token to be rejected")
	}
}

func TestValidateRejectsPublicAddressWithoutAuthToken(t *testing.T) {
	cfg := config.Config{
		Addr:                      "0.0.0.0:8080",
		LogMaxBytes:               10 * 1024 * 1024,
		TaskTimeoutSeconds:        2 * 60 * 60,
		LoginFailureLimit:         5,
		LoginFailureWindowSeconds: 5 * 60,
		APIWriteLimit:             120,
		APIWriteRateWindowSeconds: 60,
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected public address without auth token to be rejected")
	}
}

func TestValidateAllowsPublicAddressWithAuthToken(t *testing.T) {
	cfg := config.Config{
		Addr:                      "0.0.0.0:8080",
		AuthToken:                 "secret-token",
		LogMaxBytes:               10 * 1024 * 1024,
		TaskTimeoutSeconds:        2 * 60 * 60,
		LoginFailureLimit:         5,
		LoginFailureWindowSeconds: 5 * 60,
		APIWriteLimit:             120,
		APIWriteRateWindowSeconds: 60,
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected public address with auth token to be valid: %v", err)
	}
}

func TestValidateRejectsInvalidLogMaxBytes(t *testing.T) {
	cfg := config.Config{
		Addr:                      "127.0.0.1:8080",
		LogMaxBytes:               0,
		TaskTimeoutSeconds:        2 * 60 * 60,
		LoginFailureLimit:         5,
		LoginFailureWindowSeconds: 5 * 60,
		APIWriteLimit:             120,
		APIWriteRateWindowSeconds: 60,
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected non-positive log max bytes to be rejected")
	}
}

func TestValidateRejectsInvalidTaskTimeout(t *testing.T) {
	cfg := config.Config{
		Addr:                      "127.0.0.1:8080",
		LogMaxBytes:               10 * 1024 * 1024,
		TaskTimeoutSeconds:        0,
		LoginFailureLimit:         5,
		LoginFailureWindowSeconds: 5 * 60,
		APIWriteLimit:             120,
		APIWriteRateWindowSeconds: 60,
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected non-positive task timeout to be rejected")
	}
}

func TestValidateRejectsInvalidLoginFailureLimit(t *testing.T) {
	cfg := config.Config{
		Addr:                      "127.0.0.1:8080",
		LogMaxBytes:               10 * 1024 * 1024,
		TaskTimeoutSeconds:        2 * 60 * 60,
		LoginFailureLimit:         0,
		LoginFailureWindowSeconds: 5 * 60,
		APIWriteLimit:             120,
		APIWriteRateWindowSeconds: 60,
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected non-positive login failure limit to be rejected")
	}
}

func TestValidateRejectsInvalidLoginFailureWindow(t *testing.T) {
	cfg := config.Config{
		Addr:                      "127.0.0.1:8080",
		LogMaxBytes:               10 * 1024 * 1024,
		TaskTimeoutSeconds:        2 * 60 * 60,
		LoginFailureLimit:         5,
		LoginFailureWindowSeconds: 0,
		APIWriteLimit:             120,
		APIWriteRateWindowSeconds: 60,
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected non-positive login failure window to be rejected")
	}
}

func TestValidateRejectsInvalidAPIWriteLimit(t *testing.T) {
	cfg := config.Config{
		Addr:                      "127.0.0.1:8080",
		LogMaxBytes:               10 * 1024 * 1024,
		TaskTimeoutSeconds:        2 * 60 * 60,
		LoginFailureLimit:         5,
		LoginFailureWindowSeconds: 5 * 60,
		APIWriteLimit:             0,
		APIWriteRateWindowSeconds: 60,
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected non-positive API write limit to be rejected")
	}
}

func TestValidateRejectsInvalidAPIWriteRateWindow(t *testing.T) {
	cfg := config.Config{
		Addr:                      "127.0.0.1:8080",
		LogMaxBytes:               10 * 1024 * 1024,
		TaskTimeoutSeconds:        2 * 60 * 60,
		LoginFailureLimit:         5,
		LoginFailureWindowSeconds: 5 * 60,
		APIWriteLimit:             120,
		APIWriteRateWindowSeconds: 0,
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected non-positive API write rate window to be rejected")
	}
}

func TestValidateRejectsPartialTLSConfig(t *testing.T) {
	cfg := config.Config{
		Addr:                      "127.0.0.1:8080",
		LogMaxBytes:               10 * 1024 * 1024,
		TaskTimeoutSeconds:        2 * 60 * 60,
		LoginFailureLimit:         5,
		LoginFailureWindowSeconds: 5 * 60,
		APIWriteLimit:             120,
		APIWriteRateWindowSeconds: 60,
		TLSCertPath:               "certs/gateway.crt",
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected partial TLS config to be rejected")
	}
}

func TestValidateAllowsCompleteTLSConfig(t *testing.T) {
	cfg := config.Config{
		Addr:                      "127.0.0.1:8080",
		LogMaxBytes:               10 * 1024 * 1024,
		TaskTimeoutSeconds:        2 * 60 * 60,
		LoginFailureLimit:         5,
		LoginFailureWindowSeconds: 5 * 60,
		APIWriteLimit:             120,
		APIWriteRateWindowSeconds: 60,
		TLSCertPath:               "certs/gateway.crt",
		TLSKeyPath:                "certs/gateway.key",
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected complete TLS config to be valid: %v", err)
	}
}

func TestValidateRejectsMissingTelegramUserID(t *testing.T) {
	cfg := config.Config{
		Addr:                      "127.0.0.1:8080",
		LogMaxBytes:               10 * 1024 * 1024,
		TaskTimeoutSeconds:        2 * 60 * 60,
		LoginFailureLimit:         5,
		LoginFailureWindowSeconds: 5 * 60,
		APIWriteLimit:             120,
		APIWriteRateWindowSeconds: 60,
		TelegramBotToken:          "bot-token",
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected missing Telegram user ID to be rejected")
	}
}

func TestValidateAllowsCompleteTelegramConfig(t *testing.T) {
	cfg := config.Config{
		Addr:                      "127.0.0.1:8080",
		LogMaxBytes:               10 * 1024 * 1024,
		TaskTimeoutSeconds:        2 * 60 * 60,
		LoginFailureLimit:         5,
		LoginFailureWindowSeconds: 5 * 60,
		APIWriteLimit:             120,
		APIWriteRateWindowSeconds: 60,
		TelegramBotToken:          "bot-token",
		TelegramUserID:            12345,
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected complete Telegram config to be valid: %v", err)
	}
}

