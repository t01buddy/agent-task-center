package ai

import (
	"encoding/json"
	"fmt"
	"strings"
)

// WorkflowDef is a workflow name + natural-language definition.
type WorkflowDef struct {
	Name       string
	Definition string
}

// ClassifyInput carries everything the LLM needs to make a decision.
type ClassifyInput struct {
	Title        string
	ContextJSON  string
	Workflows    []WorkflowDef // all known workflows (for detection); may be 1 if workflow already known
	CurrentStep  string        // existing task step, empty if new
	CurrentStatus string       // existing task status, empty if new
}

// ClassifyResult is the structured LLM output.
type ClassifyResult struct {
	WorkflowName string `json:"workflow_name"`
	Step         string `json:"step"`
	Domain       string `json:"domain"`
	Priority     int    `json:"priority"`
	Reasoning    string `json:"reasoning"`
}

const classifySystemPrompt = `You are a workflow classifier for Agent Task Center.
Given an incoming work item and available workflow definitions, decide:
- which workflow it belongs to (workflow_name)
- what the next step should be (step)
- suggested worker domain (domain)
- priority from 0 (lowest) to 100 (highest)
- brief reasoning

Consider the current task state if provided; advance the step when context has changed.
Respond with JSON only — no markdown, no explanation outside the JSON object:
{
  "workflow_name": "<exact name from list>",
  "step": "<next step name>",
  "domain": "<suggested worker domain>",
  "priority": <0-100>,
  "reasoning": "<brief explanation>"
}`

// Classify calls the provider and returns a structured classification result.
func Classify(p Provider, input ClassifyInput) (ClassifyResult, error) {
	var sb strings.Builder

	if len(input.Workflows) > 1 {
		sb.WriteString("Available workflows:\n")
		for _, w := range input.Workflows {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", w.Name, w.Definition))
		}
	} else if len(input.Workflows) == 1 {
		sb.WriteString(fmt.Sprintf("Workflow: %s\nDefinition: %s\n", input.Workflows[0].Name, input.Workflows[0].Definition))
	}

	if input.CurrentStep != "" || input.CurrentStatus != "" {
		sb.WriteString(fmt.Sprintf("\nCurrent task state: step=%s, status=%s\n",
			orNone(input.CurrentStep), orNone(input.CurrentStatus)))
	} else {
		sb.WriteString("\nCurrent task state: none (new task)\n")
	}

	sb.WriteString(fmt.Sprintf("\nWork item:\nTitle: %s\n", input.Title))
	if input.ContextJSON != "" && input.ContextJSON != "null" {
		sb.WriteString(fmt.Sprintf("Context: %s\n", input.ContextJSON))
	}

	raw, err := p.Complete(classifySystemPrompt, sb.String())
	if err != nil {
		return ClassifyResult{}, fmt.Errorf("classify: llm call: %w", err)
	}

	raw = strings.TrimSpace(raw)
	// Strip markdown code fences if the model wraps despite json_object mode
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var result ClassifyResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return ClassifyResult{}, fmt.Errorf("classify: parse response: %w (raw: %s)", err, raw)
	}
	if result.WorkflowName == "" {
		return ClassifyResult{}, fmt.Errorf("classify: empty workflow_name in response")
	}
	if result.Step == "" {
		return ClassifyResult{}, fmt.Errorf("classify: empty step in response")
	}
	return result, nil
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}
