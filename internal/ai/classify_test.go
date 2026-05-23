package ai

import (
	"errors"
	"strings"
	"testing"

	"github.com/t01buddy/agent-task-center/internal/config"
)

type mockProvider struct {
	response string
	err      error
}

func (m *mockProvider) Complete(_, _ string) (string, error) {
	return m.response, m.err
}

type capturingProvider struct {
	fn func(system, user string) (string, error)
}

func (c *capturingProvider) Complete(system, user string) (string, error) {
	return c.fn(system, user)
}

func TestClassify_ValidJSON(t *testing.T) {
	p := &mockProvider{response: `{"workflow_name":"bug-fix","step":"triage","domain":"backend","priority":80,"reasoning":"new bug"}`}
	result, err := Classify(p, ClassifyInput{
		Title:     "Fix login bug",
		Workflows: []WorkflowDef{{Name: "bug-fix", Definition: "fix bugs"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.WorkflowName != "bug-fix" {
		t.Errorf("workflow_name: got %q", result.WorkflowName)
	}
	if result.Step != "triage" {
		t.Errorf("step: got %q", result.Step)
	}
	if result.Priority != 80 {
		t.Errorf("priority: got %d", result.Priority)
	}
}

func TestClassify_FencedJSON(t *testing.T) {
	p := &mockProvider{response: "```json\n{\"workflow_name\":\"bug-fix\",\"step\":\"implement\",\"domain\":\"backend\",\"priority\":50,\"reasoning\":\"ok\"}\n```"}
	result, err := Classify(p, ClassifyInput{
		Title:     "Fix thing",
		Workflows: []WorkflowDef{{Name: "bug-fix", Definition: "fix bugs"}},
	})
	if err != nil {
		t.Fatalf("fence stripping failed: %v", err)
	}
	if result.Step != "implement" {
		t.Errorf("step: got %q", result.Step)
	}
}

func TestClassify_MissingWorkflowName(t *testing.T) {
	p := &mockProvider{response: `{"step":"triage","domain":"backend","priority":50,"reasoning":"ok"}`}
	_, err := Classify(p, ClassifyInput{Title: "x", Workflows: []WorkflowDef{{Name: "bug-fix", Definition: "d"}}})
	if err == nil || !strings.Contains(err.Error(), "empty workflow_name") {
		t.Errorf("expected empty workflow_name error, got: %v", err)
	}
}

func TestClassify_MissingStep(t *testing.T) {
	p := &mockProvider{response: `{"workflow_name":"bug-fix","domain":"backend","priority":50,"reasoning":"ok"}`}
	_, err := Classify(p, ClassifyInput{Title: "x", Workflows: []WorkflowDef{{Name: "bug-fix", Definition: "d"}}})
	if err == nil || !strings.Contains(err.Error(), "empty step") {
		t.Errorf("expected empty step error, got: %v", err)
	}
}

func TestClassify_InvalidJSON(t *testing.T) {
	p := &mockProvider{response: "not json at all"}
	_, err := Classify(p, ClassifyInput{Title: "x", Workflows: []WorkflowDef{{Name: "bug-fix", Definition: "d"}}})
	if err == nil || !strings.Contains(err.Error(), "parse response") {
		t.Errorf("expected parse error, got: %v", err)
	}
}

func TestClassify_ProviderError(t *testing.T) {
	p := &mockProvider{err: errors.New("network timeout")}
	_, err := Classify(p, ClassifyInput{Title: "x", Workflows: []WorkflowDef{{Name: "bug-fix", Definition: "d"}}})
	if err == nil || !strings.Contains(err.Error(), "llm call") {
		t.Errorf("expected llm call error, got: %v", err)
	}
}

func TestClassify_CurrentStateInPrompt(t *testing.T) {
	var capturedUser string
	p := &capturingProvider{fn: func(_, user string) (string, error) {
		capturedUser = user
		return `{"workflow_name":"bug-fix","step":"implement","domain":"backend","priority":60,"reasoning":"advancing"}`, nil
	}}
	_, err := Classify(p, ClassifyInput{
		Title:         "Fix login bug",
		Workflows:     []WorkflowDef{{Name: "bug-fix", Definition: "fix bugs"}},
		CurrentStep:   "triage",
		CurrentStatus: "completed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(capturedUser, "step=triage") {
		t.Errorf("current step not in prompt:\n%s", capturedUser)
	}
}

func TestNewProvider_OpenAI(t *testing.T) {
	cfg := config.Config{LLMProvider: "openai", LLMAPIKey: "sk-test", LLMBaseURL: "https://api.openai.com/v1", LLMModel: "gpt-4o-mini"}
	p, err := NewProvider(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.(*OpenAIProvider); !ok {
		t.Errorf("expected *OpenAIProvider, got %T", p)
	}
}

func TestNewProvider_EmptyDefaultsToOpenAI(t *testing.T) {
	cfg := config.Config{}
	p, err := NewProvider(cfg)
	if err != nil {
		t.Fatal(err)
	}
	op, ok := p.(*OpenAIProvider)
	if !ok {
		t.Fatalf("expected *OpenAIProvider, got %T", p)
	}
	if op.BaseURL != "https://api.openai.com/v1" {
		t.Errorf("default base URL: %s", op.BaseURL)
	}
	if op.Model != "gpt-4o-mini" {
		t.Errorf("default model: %s", op.Model)
	}
}

func TestNewProvider_Codex(t *testing.T) {
	cfg := config.Config{LLMProvider: "codex"}
	p, err := NewProvider(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.(*CodexCLIProvider); !ok {
		t.Errorf("expected *CodexCLIProvider, got %T", p)
	}
}

func TestNewProvider_Unknown(t *testing.T) {
	cfg := config.Config{LLMProvider: "grok"}
	_, err := NewProvider(cfg)
	if err == nil || !strings.Contains(err.Error(), "unknown LLM provider") {
		t.Errorf("expected unknown provider error, got: %v", err)
	}
}
