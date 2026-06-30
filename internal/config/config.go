package config

import (
	"errors"
	"net"
	"os"
	"strconv"
)

const productionAuthTokenMinLength = 32

type Config struct {
	Env                       string
	Addr                      string
	DBPath                    string
	AuthToken                 string
	LogPath                   string
	LogMaxBytes               int64
	TaskTimeoutSeconds        int64
	LoginFailureLimit         int64
	LoginFailureWindowSeconds int64
	APIWriteLimit             int64
	APIWriteRateWindowSeconds int64
	TLSCertPath               string
	TLSKeyPath                string
	TelegramBotToken          string
	TelegramUserID            int64
	OpenRouterAPIKey          string
	OpenRouterModel           string
	OpenRouterBaseURL         string
}

func Load() Config {
	return Config{
		Env:                       os.Getenv("PAIA_ENV"),
		Addr:                      envOrDefault("PAIA_ADDR", "127.0.0.1:8080"),
		DBPath:                    envOrDefault("PAIA_DB_PATH", "data/gateway.db"),
		AuthToken:                 os.Getenv("PAIA_AUTH_TOKEN"),
		LogPath:                   os.Getenv("PAIA_LOG_PATH"),
		LogMaxBytes:               envInt64OrDefault("PAIA_LOG_MAX_BYTES", 10*1024*1024),
		TaskTimeoutSeconds:        envInt64OrDefault("PAIA_TASK_TIMEOUT_SECONDS", 2*60*60),
		LoginFailureLimit:         envInt64OrDefault("PAIA_LOGIN_FAILURE_LIMIT", 5),
		LoginFailureWindowSeconds: envInt64OrDefault("PAIA_LOGIN_FAILURE_WINDOW_SECONDS", 5*60),
		APIWriteLimit:             envInt64OrDefault("PAIA_API_WRITE_LIMIT", 120),
		APIWriteRateWindowSeconds: envInt64OrDefault("PAIA_API_WRITE_RATE_WINDOW_SECONDS", 60),
		TLSCertPath:               os.Getenv("PAIA_TLS_CERT_PATH"),
		TLSKeyPath:                os.Getenv("PAIA_TLS_KEY_PATH"),
		TelegramBotToken:          os.Getenv("PAIA_TELEGRAM_BOT_TOKEN"),
		TelegramUserID:            envInt64OrDefault("PAIA_TELEGRAM_USER_ID", 0),
		OpenRouterAPIKey:          os.Getenv("OPENROUTER_API_KEY"),
		OpenRouterModel:           envOrDefault("OPENROUTER_MODEL", "openrouter/free"),
		OpenRouterBaseURL:         envOrDefault("OPENROUTER_BASE_URL", "https://openrouter.ai/api/v1"),
	}
}

func (c Config) Validate() error {
	if c.Env != "" && c.Env != "development" && c.Env != "production" {
		return errors.New("PAIA_ENV must be empty, development, or production")
	}
	host, _, err := net.SplitHostPort(c.Addr)
	if err != nil {
		return err
	}
	if c.AuthToken == "" && !isLocalHost(host) {
		return errors.New("PAIA_AUTH_TOKEN is required when binding outside localhost")
	}
	if c.Env == "production" && c.AuthToken == "" {
		return errors.New("PAIA_AUTH_TOKEN is required when PAIA_ENV=production")
	}
	if c.Env == "production" && len(c.AuthToken) < productionAuthTokenMinLength {
		return errors.New("PAIA_AUTH_TOKEN must be at least 32 characters when PAIA_ENV=production")
	}
	if c.LogMaxBytes <= 0 {
		return errors.New("PAIA_LOG_MAX_BYTES must be greater than zero")
	}
	if c.TaskTimeoutSeconds <= 0 {
		return errors.New("PAIA_TASK_TIMEOUT_SECONDS must be greater than zero")
	}
	if c.LoginFailureLimit <= 0 {
		return errors.New("PAIA_LOGIN_FAILURE_LIMIT must be greater than zero")
	}
	if c.LoginFailureWindowSeconds <= 0 {
		return errors.New("PAIA_LOGIN_FAILURE_WINDOW_SECONDS must be greater than zero")
	}
	if c.APIWriteLimit <= 0 {
		return errors.New("PAIA_API_WRITE_LIMIT must be greater than zero")
	}
	if c.APIWriteRateWindowSeconds <= 0 {
		return errors.New("PAIA_API_WRITE_RATE_WINDOW_SECONDS must be greater than zero")
	}
	if (c.TLSCertPath == "") != (c.TLSKeyPath == "") {
		return errors.New("PAIA_TLS_CERT_PATH and PAIA_TLS_KEY_PATH must be set together")
	}
	if c.TelegramBotToken != "" && c.TelegramUserID <= 0 {
		return errors.New("PAIA_TELEGRAM_USER_ID is required and must be a positive integer when PAIA_TELEGRAM_BOT_TOKEN is set")
	}
	return nil
}

func envOrDefault(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func envInt64OrDefault(key string, fallback int64) int64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return -1
	}
	return parsed
}

func isLocalHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
