package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OpenAIProvider calls any OpenAI-compatible chat completions endpoint.
type OpenAIProvider struct {
	BaseURL string
	APIKey  string
	Model   string
}

func (p *OpenAIProvider) Complete(systemPrompt, userMessage string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"model": p.Model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userMessage},
		},
		"response_format": map[string]string{"type": "json_object"},
	})

	req, err := http.NewRequest(http.MethodPost, p.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("openai: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("openai: request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("openai: read body: %w", err)
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("openai: status %d: %s", resp.StatusCode, raw)
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("openai: parse response: %w", err)
	}
	if out.Error != nil {
		return "", fmt.Errorf("openai: api error: %s", out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("openai: no choices in response")
	}
	return out.Choices[0].Message.Content, nil
}
