package ai

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// CodexCLIProvider runs completions via `codex exec --full-auto --quiet`.
type CodexCLIProvider struct {
	Model string // passed as --model flag if non-empty
}

func (p *CodexCLIProvider) Complete(systemPrompt, userMessage string) (string, error) {
	prompt := systemPrompt + "\n\n" + userMessage

	args := []string{"exec", "--full-auto", "--quiet", prompt}
	if p.Model != "" {
		args = append([]string{"exec", "--full-auto", "--quiet", "--model", p.Model, prompt}, args[4:]...)
	}

	cmd := exec.Command("codex", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("codex: run: %w\nstderr: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}
