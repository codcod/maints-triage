package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Options configures a cursor-agent invocation.
type Options struct {
	APIKey    string
	Model     string
	Workspace string
}

// jsonResponse is the top-level shape returned by cursor-agent --output-format json.
type jsonResponse struct {
	Text    string `json:"text"`
	Message string `json:"message"` // fallback field name used by some versions
	Result  string `json:"result"`  // used by cursor-agent result envelope
}

// Run invokes cursor-agent in headless (--print) mode with the given prompt and workspace.
// It returns the text content of the agent's response.
// The API key is passed via the CURSOR_API_KEY environment variable to avoid
// exposing it in the process argument list.
func Run(ctx context.Context, prompt string, opts Options) (string, error) {
	args := []string{
		"--print",
		"--output-format", "json",
		"--force", // auto-trust the workspace directory
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.Workspace != "" {
		args = append(args, "--workspace", opts.Workspace)
	}
	args = append(args, prompt)

	cmd := exec.CommandContext(ctx, "cursor-agent", args...)
	if opts.APIKey != "" {
		cmd.Env = append(cmd.Environ(), "CURSOR_API_KEY="+opts.APIKey)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("cursor-agent failed: %s", detail)
	}

	return parseOutput(stdout.Bytes())
}

// parseOutput extracts the text content from cursor-agent's JSON output.
// The output may be a single JSON object or a stream of newline-delimited JSON objects.
func parseOutput(data []byte) (string, error) {
	raw := strings.TrimSpace(string(data))
	if raw == "" {
		return "", fmt.Errorf("cursor-agent produced no output")
	}

	// Try parsing as a single JSON object first
	var single jsonResponse
	if err := json.Unmarshal([]byte(raw), &single); err == nil {
		if t := coalesce(single.Text, single.Message, single.Result); t != "" {
			return t, nil
		}
	}

	// Fall back to newline-delimited JSON (stream-json format): collect last non-empty text
	var lastText string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var obj jsonResponse
		if err := json.Unmarshal([]byte(line), &obj); err == nil {
			if t := coalesce(obj.Text, obj.Message, obj.Result); t != "" {
				lastText = t
			}
		}
	}
	if lastText != "" {
		return lastText, nil
	}

	// Last resort: return raw output as-is
	return raw, nil
}

func coalesce(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
