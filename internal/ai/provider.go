// Package ai provides LLM provider abstractions for workflow classification.
package ai

import (
	"fmt"

	"github.com/t01buddy/agent-task-center/internal/config"
)

// Provider is the minimal interface for LLM completions.
type Provider interface {
	// Complete sends a chat completion request and returns the model's text response.
	Complete(systemPrompt, userMessage string) (string, error)
}

// NewProvider constructs the provider selected by cfg.LLMProvider.
// Supported values: "openai" (default), "codex".
func NewProvider(cfg config.Config) (Provider, error) {
	switch cfg.LLMProvider {
	case "codex":
		return &CodexCLIProvider{Model: cfg.CodexModel}, nil
	case "", "openai":
		baseURL := cfg.LLMBaseURL
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
		model := cfg.LLMModel
		if model == "" {
			model = "gpt-4o-mini"
		}
		return &OpenAIProvider{
			BaseURL: baseURL,
			APIKey:  cfg.LLMAPIKey,
			Model:   model,
		}, nil
	default:
		return nil, fmt.Errorf("unknown LLM provider %q; supported: openai, codex", cfg.LLMProvider)
	}
}
