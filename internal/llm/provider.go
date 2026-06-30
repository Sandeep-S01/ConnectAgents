package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

type LLMProvider interface {
	GenerateResponse(ctx context.Context, prompt string) (string, error)
}

type OpenRouterConfig struct {
	APIKey  string
	BaseURL string
	Model   string
}

type OpenRouterProvider struct {
	cfg    OpenRouterConfig
	client *http.Client
}

func NewOpenRouterProvider(cfg OpenRouterConfig) *OpenRouterProvider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://openrouter.ai/api/v1"
	}
	if cfg.Model == "" {
		cfg.Model = "openrouter/free"
	}
	return &OpenRouterProvider{
		cfg: cfg,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionsRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatCompletionsResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error"`
}

func (p *OpenRouterProvider) GenerateResponse(ctx context.Context, prompt string) (string, error) {
	apiKey := p.cfg.APIKey
	if ctxKey, ok := ctx.Value("openrouter_api_key").(string); ok && ctxKey != "" {
		apiKey = ctxKey
	}
	if apiKey == "" {
		return "", errors.New("openrouter api key is missing")
	}

	model := p.cfg.Model
	if ctxModel, ok := ctx.Value("openrouter_model").(string); ok && ctxModel != "" {
		model = ctxModel
	}

	reqBody := chatCompletionsRequest{
		Model: model,
		Messages: []chatMessage{
			{Role: "user", Content: prompt},
		},
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := p.cfg.BaseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("HTTP-Referer", "https://github.com/google/personal-ai-assistant")
	req.Header.Set("X-Title", "Personal AI Assistant Gateway")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var apiErr chatCompletionsResponse
		if json.Unmarshal(bodyBytes, &apiErr) == nil && apiErr.Error.Message != "" {
			return "", fmt.Errorf("openrouter error (status %d): %s", resp.StatusCode, apiErr.Error.Message)
		}
		return "", fmt.Errorf("openrouter returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var chatResp chatCompletionsResponse
	if err := json.Unmarshal(bodyBytes, &chatResp); err != nil {
		return "", fmt.Errorf("unmarshal response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return "", errors.New("no response choices returned from openrouter")
	}

	return chatResp.Choices[0].Message.Content, nil
}
