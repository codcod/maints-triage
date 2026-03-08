package triage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codcod/maints-triage/internal/agent"
	"github.com/codcod/maints-triage/internal/config"
	"github.com/codcod/maints-triage/internal/jira"
)

const (
	defaultChecklistFile = "checklist.md"
	defaultPromptFile    = "triage-prompt.md"
	promptKeyPlaceholder = "{{ISSUE_KEY}}"
)

// Options controls a triage run.
type Options struct {
	ChecklistPath string
	PromptPath    string
	Model         string
	OutputFormat  string // "text" or "json"
}

// Result holds the triage outcome for a single issue.
type Result struct {
	IssueKey   string      `json:"issue_key"`
	Summary    string      `json:"summary"`
	TriagedAt  time.Time   `json:"triaged_at"`
	Report     string      `json:"report"`
	Evaluation *Evaluation `json:"evaluation,omitempty"`
	Warnings   []string    `json:"warnings,omitempty"`
	Error      string      `json:"error,omitempty"`
}

// triageDeps groups the resolved dependencies shared across triageOne calls.
type triageDeps struct {
	checklistData  []byte
	promptTemplate []byte
	mappings       []jira.FieldMapping
	jiraClient     *jira.Client
	apiKey         string
}

// triageHome returns the triage configuration directory, in priority order:
//  1. $TRIAGE_HOME if set
//  2. $XDG_CONFIG_HOME/triage  (falls back to ~/.config/triage)
func triageHome() (string, error) {
	if th := os.Getenv("TRIAGE_HOME"); th != "" {
		return th, nil
	}
	xdgConfigHome := os.Getenv("XDG_CONFIG_HOME")
	if xdgConfigHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		xdgConfigHome = filepath.Join(home, ".config")
	}
	return filepath.Join(xdgConfigHome, "triage"), nil
}

// loadFieldsMappings reads the optional fields-mapping.json from triage home.
// If the file does not exist an empty slice is returned without error.
func loadFieldsMappings() ([]jira.FieldMapping, error) {
	th, err := triageHome()
	if err != nil {
		return nil, err
	}
	p := filepath.Join(th, "fields-mapping.json")
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read fields-mapping %q: %w", p, err)
	}
	var mappings []jira.FieldMapping
	if err := json.Unmarshal(data, &mappings); err != nil {
		return nil, fmt.Errorf("parse fields-mapping %q: %w", p, err)
	}
	return mappings, nil
}

// resolveConfigFile returns the path for a named config file, in priority order:
//  1. explicit override value (non-empty)
//  2. $TRIAGE_HOME/<name>  (defaults to $XDG_CONFIG_HOME/triage/<name>)
//  3. ./<name>
func resolveConfigFile(explicit, name string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}

	th, err := triageHome()
	if err != nil {
		return "", err
	}
	thPath := filepath.Join(th, name)
	if _, err := os.Stat(thPath); err == nil {
		return thPath, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("stat %q: %w", thPath, err)
	}

	return name, nil
}

// resolveChecklist returns the checklist path to use, in priority order:
//  1. explicit --checklist flag value
//  2. $TRIAGE_HOME/checklist.md  (defaults to $XDG_CONFIG_HOME/triage/checklist.md)
//  3. ./checklist.md
func resolveChecklist(explicit string) (string, error) {
	return resolveConfigFile(explicit, defaultChecklistFile)
}

// resolvePrompt returns the prompt template path to use, in priority order:
//  1. explicit --prompt flag value
//  2. $TRIAGE_HOME/triage-prompt.md  (defaults to $XDG_CONFIG_HOME/triage/triage-prompt.md)
//  3. ./triage-prompt.md
func resolvePrompt(explicit string) (string, error) {
	return resolveConfigFile(explicit, defaultPromptFile)
}

// Run triages one or more Jira issues and writes results to w.
func Run(ctx context.Context, issueKeys []string, cfg *config.Config, opts Options, w io.Writer) error {
	switch opts.OutputFormat {
	case "text", "json", "":
	default:
		return fmt.Errorf("unsupported output format %q (use text or json)", opts.OutputFormat)
	}

	checklistPath, err := resolveChecklist(opts.ChecklistPath)
	if err != nil {
		return err
	}

	checklistData, err := os.ReadFile(checklistPath)
	if err != nil {
		return fmt.Errorf("read checklist %q: %w", checklistPath, err)
	}

	promptPath, err := resolvePrompt(opts.PromptPath)
	if err != nil {
		return err
	}

	promptTemplate, err := os.ReadFile(promptPath)
	if err != nil {
		return fmt.Errorf("read prompt %q: %w", promptPath, err)
	}

	mappings, err := loadFieldsMappings()
	if err != nil {
		return err
	}

	deps := triageDeps{
		checklistData:  checklistData,
		promptTemplate: promptTemplate,
		mappings:       mappings,
		jiraClient:     jira.NewClient(cfg.JiraURL, cfg.JiraUsername, cfg.JiraAPIToken),
		apiKey:         cfg.CursorAPIKey,
	}

	for _, key := range issueKeys {
		key = strings.ToUpper(strings.TrimSpace(key))
		_, _ = fmt.Fprintf(w, "Triaging %s...\n", key)

		result := triageOne(ctx, key, deps, opts)
		printResult(w, result, opts.OutputFormat)
	}

	return nil
}

func triageOne(ctx context.Context, key string, deps triageDeps, opts Options) Result {
	result := Result{
		IssueKey:  key,
		TriagedAt: time.Now(),
	}

	issue, err := deps.jiraClient.FetchIssue(ctx, key, deps.mappings)
	if err != nil {
		result.Error = fmt.Sprintf("failed to fetch issue: %s", err)
		return result
	}
	result.Summary = issue.Summary

	workDir := filepath.Join("triaged-maints", key)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		result.Error = fmt.Sprintf("create workspace directory: %s", err)
		return result
	}

	issueFile := filepath.Join(workDir, "issue-"+key+".md")
	if err := writeIssueMarkdown(issueFile, issue); err != nil {
		result.Error = fmt.Sprintf("write issue file: %s", err)
		return result
	}

	checklistDst := filepath.Join(workDir, defaultChecklistFile)
	if err := os.WriteFile(checklistDst, deps.checklistData, 0o644); err != nil {
		result.Error = fmt.Sprintf("write checklist file: %s", err)
		return result
	}

	prompt := strings.ReplaceAll(string(deps.promptTemplate), promptKeyPlaceholder, key)
	agentOutput, err := agent.Run(ctx, prompt, agent.Options{
		APIKey:    deps.apiKey,
		Model:     opts.Model,
		Workspace: workDir,
	})
	if err != nil {
		result.Error = fmt.Sprintf("agent error: %s", err)
		return result
	}

	// Attempt to parse structured JSON from the agent output.
	// On success, render the report from the validated evaluation.
	// On failure, degrade gracefully to the raw agent text.
	if eval, _ := parseEvaluation(agentOutput); eval != nil {
		result.Evaluation = eval
		result.Report = renderEvaluationMarkdown(eval)
		result.Warnings = validateEvaluation(eval)
	} else {
		result.Report = agentOutput
	}

	reportFile := filepath.Join(workDir, "report-"+key+".md")
	if err := writeReport(reportFile, result); err != nil {
		result.Error = fmt.Sprintf("write report file: %s", err)
	}

	return result
}

// fmtWriter accumulates the first write error so callers can check errors
// once after a series of Fprintf calls rather than after each one.
type fmtWriter struct {
	w   io.Writer
	err error
}

func (fw *fmtWriter) printf(format string, args ...any) {
	if fw.err == nil {
		_, fw.err = fmt.Fprintf(fw.w, format, args...)
	}
}

func writeReport(path string, r Result) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	fw := &fmtWriter{w: f}
	fw.printf("# Triage Report: %s\n\n", r.IssueKey)
	fw.printf("**Summary:** %s\n\n", r.Summary)
	fw.printf("**Triaged at:** %s\n\n", r.TriagedAt.Format(time.RFC3339))
	fw.printf("---\n\n")
	fw.printf("%s\n", r.Report)
	if fw.err != nil {
		return fw.err
	}
	return f.Close()
}

func writeIssueMarkdown(path string, issue *jira.Issue) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	fw := &fmtWriter{w: f}
	fw.printf("# Jira Issue: %s\n\n", issue.Key)
	fw.printf("## Summary\n%s\n\n", issue.Summary)
	fw.printf("## Metadata\n\n")
	fw.printf("| Field             | Value |\n")
	fw.printf("|-------------------|-------|\n")
	fw.printf("| Status            | %s |\n", issue.Status)
	fw.printf("| Priority          | %s |\n", issue.Priority)
	fw.printf("| Reporter          | %s |\n", issue.Reporter)
	fw.printf("| Assignee          | %s |\n", issue.Assignee)
	fw.printf("| Components        | %s |\n", strings.Join(issue.Components, ", "))
	fw.printf("| Affected Versions | %s |\n", strings.Join(issue.AffectedVersions, ", "))
	fw.printf("| Fix Versions      | %s |\n", strings.Join(issue.FixVersions, ", "))
	fw.printf("| Labels            | %s |\n", strings.Join(issue.Labels, ", "))
	for _, fv := range issue.ExtraFields {
		fw.printf("| %-17s | %s |\n", fv.Field, fv.Value)
	}
	fw.printf("\n")
	fw.printf("## Description\n\n%s\n\n", issue.Description)

	if len(issue.Comments) > 0 {
		fw.printf("## Comments\n\n")
		for _, c := range issue.Comments {
			fw.printf("### %s (%s)\n\n%s\n\n", c.Author, c.Created, c.Body)
		}
	}
	if fw.err != nil {
		return fw.err
	}
	return f.Close()
}

func printResult(w io.Writer, r Result, format string) {
	if format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(r)
		return
	}

	fw := &fmtWriter{w: w}
	fw.printf("\n%s\n", strings.Repeat("─", 60))
	fw.printf("Issue:   %s\n", r.IssueKey)
	fw.printf("Summary: %s\n", r.Summary)
	fw.printf("Triaged: %s\n", r.TriagedAt.Format(time.RFC3339))
	fw.printf("%s\n\n", strings.Repeat("─", 60))

	if r.Error != "" {
		fw.printf("ERROR: %s\n\n", r.Error)
		return
	}

	fw.printf("%s\n\n", r.Report)

	if len(r.Warnings) > 0 {
		fw.printf("⚠️  Validation warnings:\n")
		for _, warning := range r.Warnings {
			fw.printf("  - %s\n", warning)
		}
		fw.printf("\n")
	}
}
