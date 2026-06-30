package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"personal-ai-assistant/internal/config"
	"personal-ai-assistant/internal/gateway"
	"personal-ai-assistant/internal/llm"
	"personal-ai-assistant/internal/logging"
	"personal-ai-assistant/internal/store"
	"personal-ai-assistant/internal/telegram"
)

func main() {
	cfg := config.Load()

	if len(os.Args) > 1 && os.Args[1] == "test-ai" {
		log.Printf("Testing OpenRouter connection...")
		log.Printf("Base URL: %s", cfg.OpenRouterBaseURL)
		log.Printf("Model: %s", cfg.OpenRouterModel)
		if cfg.OpenRouterAPIKey == "" {
			log.Fatalf("Error: OPENROUTER_API_KEY environment variable is not set")
		}

		llmProv := llm.NewOpenRouterProvider(llm.OpenRouterConfig{
			APIKey:  cfg.OpenRouterAPIKey,
			BaseURL: cfg.OpenRouterBaseURL,
			Model:   cfg.OpenRouterModel,
		})

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		response, err := llmProv.GenerateResponse(ctx, "Hello! Please reply with exactly: 'OpenRouter connection successful!'")
		if err != nil {
			log.Fatalf("Error generating response: %v", err)
		}

		log.Printf("Response received successfully!")
		log.Printf("Response: %s", response)
		os.Exit(0)
	}

	if err := cfg.Validate(); err != nil {
		log.Fatalf("invalid config: %v", err)
	}
	logOutput := os.Stdout
	if cfg.LogPath != "" {
		logWriter, err := logging.NewRotatingFileWriter(cfg.LogPath, cfg.LogMaxBytes)
		if err != nil {
			log.Fatalf("open log file: %v", err)
		}
		defer logWriter.Close()
		log.SetOutput(logWriter)
		slog.SetDefault(slog.New(slog.NewJSONHandler(logWriter, nil)))
	} else {
		slog.SetDefault(slog.New(slog.NewJSONHandler(logOutput, nil)))
	}
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		log.Fatalf("create database directory: %v", err)
	}

	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, cfg.DBPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer db.Close()

	interrupted, err := db.MarkRunningTasksInterrupted(ctx)
	if err != nil {
		log.Fatalf("recover running tasks: %v", err)
	}
	if interrupted > 0 {
		slog.Info("recovered_interrupted_tasks", "count", interrupted)
	}

	var bot *telegram.Bot
	if cfg.TelegramBotToken != "" {
		bot = telegram.NewBot(cfg.TelegramBotToken, cfg.TelegramUserID, cfg.Addr, cfg.AuthToken)
	}

	llmProv := llm.NewOpenRouterProvider(llm.OpenRouterConfig{
		APIKey:  cfg.OpenRouterAPIKey,
		BaseURL: cfg.OpenRouterBaseURL,
		Model:   cfg.OpenRouterModel,
	})

	server := newHTTPServer(
		cfg.Addr,
		gateway.NewServer(db, gateway.Options{
			AuthToken:          cfg.AuthToken,
			LLMProvider:        llmProv,
			TaskTimeout:        time.Duration(cfg.TaskTimeoutSeconds) * time.Second,
			LoginFailureLimit:  int(cfg.LoginFailureLimit),
			LoginFailureWindow: time.Duration(cfg.LoginFailureWindowSeconds) * time.Second,
			APIWriteLimit:      int(cfg.APIWriteLimit),
			APIWriteRateWindow: time.Duration(cfg.APIWriteRateWindowSeconds) * time.Second,
			OnTaskEvent: func(e store.TaskEvent) {
				if bot != nil {
					bot.HandleEvent(e)
				}
			},
		}),
	)

	botCtx, botCancel := context.WithCancel(context.Background())
	defer botCancel()

	if bot != nil {
		go bot.Start(botCtx)
	}

	go func() {
		scheme := "http"
		serve := server.ListenAndServe
		if cfg.TLSCertPath != "" {
			scheme = "https"
			serve = func() error {
				return server.ListenAndServeTLS(cfg.TLSCertPath, cfg.TLSKeyPath)
			}
		}
		log.Printf("gateway listening on %s://%s", scheme, cfg.Addr)
		if err := serve(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
	}
}
