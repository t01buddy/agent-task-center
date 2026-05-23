package ai

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// CodexCLIProvider runs completions via `codex exec`.
type CodexCLIProvider struct {
	Model string // passed via -c model=<value> if non-empty
}

func (p *CodexCLIProvider) Complete(systemPrompt, userMessage string) (string, error) {
	prompt := systemPrompt + "\n\n" + userMessage

	// Write output to a temp file so we capture only the final message.
	tmp, err := os.CreateTemp("", "atc-codex-*.txt")
	if err != nil {
		return "", fmt.Errorf("codex: tmp file: %w", err)
	}
	tmp.Close()
	defer os.Remove(tmp.Name())

	args := []string{"exec", "--dangerously-bypass-approvals-and-sandbox", "-o", tmp.Name()}
	if p.Model != "" {
		args = append(args, "-c", "model="+p.Model)
	}
	args = append(args, prompt)

	cmd := exec.Command("codex", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("codex: run: %w\nstderr: %s", err, strings.TrimSpace(stderr.String()))
	}

	out, err := os.ReadFile(tmp.Name())
	if err != nil {
		return "", fmt.Errorf("codex: read output: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
