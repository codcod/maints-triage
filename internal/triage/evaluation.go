package triage

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ItemStatus is the evaluation result for a single checklist item.
type ItemStatus string

const (
	StatusComplete ItemStatus = "complete"
	StatusPartial  ItemStatus = "partial"
	StatusMissing  ItemStatus = "missing"
	StatusNA       ItemStatus = "na"
)

// ChecklistItem is the structured result for a single checklist item.
type ChecklistItem struct {
	ID        int        `json:"id"`
	Title     string     `json:"title"`
	Status    ItemStatus `json:"status"`
	Evidence  string     `json:"evidence"`  // exact quote from the issue; empty for "na"
	Reasoning string     `json:"reasoning"` // one-sentence justification
}

// Evaluation is the structured output from the agent for a full triage run.
// It is populated when the agent returns well-formed JSON; callers fall back
// to the raw text report when it is nil.
type Evaluation struct {
	Items          []ChecklistItem `json:"items"`
	Summary        string          `json:"summary"`
	Verdict        string          `json:"verdict"`         // "PASS" or "FAIL"
	ReviewRequired bool            `json:"review_required"` // true when the AI flags uncertainty
}

// parseEvaluation attempts to extract a structured Evaluation from the raw
// agent output. It tries, in order:
//  1. The full output parsed directly as a JSON object.
//  2. A ```json…``` fenced block embedded anywhere in the output.
//
// Returns (nil, nil) when the output is not parseable as a valid Evaluation,
// allowing callers to degrade gracefully to the raw text.
func parseEvaluation(raw string) (*Evaluation, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	if e := tryUnmarshalEvaluation(raw); e != nil {
		return e, nil
	}

	if block := extractJSONBlock(raw); block != "" {
		if e := tryUnmarshalEvaluation(block); e != nil {
			return e, nil
		}
	}

	return nil, nil
}

// tryUnmarshalEvaluation unmarshals s into an Evaluation and returns it only
// when the result has at least one item and a non-empty verdict.
func tryUnmarshalEvaluation(s string) *Evaluation {
	var e Evaluation
	if err := json.Unmarshal([]byte(s), &e); err != nil {
		return nil
	}
	if len(e.Items) == 0 || e.Verdict == "" {
		return nil
	}
	return &e
}

// extractJSONBlock pulls the content of the first ```json…``` code fence from s.
func extractJSONBlock(s string) string {
	const openFence = "```json"
	const closeFence = "```"

	start := strings.Index(s, openFence)
	if start == -1 {
		return ""
	}
	start += len(openFence)

	end := strings.Index(s[start:], closeFence)
	if end == -1 {
		return ""
	}
	return strings.TrimSpace(s[start : start+end])
}

// validateEvaluation checks that an Evaluation is internally consistent.
// It returns a slice of human-readable warning strings; an empty slice means
// no issues were found.
func validateEvaluation(e *Evaluation) []string {
	var warnings []string

	hasProblem := false
	for _, item := range e.Items {
		switch item.Status {
		case StatusComplete, StatusPartial, StatusMissing, StatusNA:
		default:
			warnings = append(warnings,
				fmt.Sprintf("item %d (%q) has unrecognised status %q", item.ID, item.Title, item.Status))
		}
		if item.Status != StatusNA && item.Evidence == "" {
			warnings = append(warnings,
				fmt.Sprintf("item %d (%q) is %s but has no evidence quote", item.ID, item.Title, item.Status))
		}
		if item.Status == StatusPartial || item.Status == StatusMissing {
			hasProblem = true
		}
	}

	switch e.Verdict {
	case "PASS", "FAIL":
	default:
		warnings = append(warnings,
			fmt.Sprintf("unrecognised verdict %q (expected PASS or FAIL)", e.Verdict))
	}
	if e.Verdict == "PASS" && hasProblem {
		warnings = append(warnings, "verdict is PASS but one or more items are partial or missing")
	}
	if e.Verdict == "FAIL" && !hasProblem {
		warnings = append(warnings, "verdict is FAIL but all items are complete or N/A")
	}

	return warnings
}

// renderEvaluationMarkdown converts a structured Evaluation to the
// human-readable markdown that is written to the report body.
func renderEvaluationMarkdown(e *Evaluation) string {
	var sb strings.Builder

	sb.WriteString("## Checklist Evaluation\n\n")
	for _, item := range e.Items {
		fmt.Fprintf(&sb, "### %d. %s — %s\n\n", item.ID, item.Title, statusLabel(item.Status))
		if item.Evidence != "" {
			fmt.Fprintf(&sb, "> %s\n\n", item.Evidence)
		}
		if item.Reasoning != "" {
			sb.WriteString(item.Reasoning + "\n\n")
		}
	}

	// Gap table — only partial / missing items.
	var gaps []ChecklistItem
	for _, item := range e.Items {
		if item.Status == StatusPartial || item.Status == StatusMissing {
			gaps = append(gaps, item)
		}
	}
	if len(gaps) > 0 {
		sb.WriteString("---\n\n## Summary of Gaps\n\n")
		sb.WriteString("| # | Item | Status |\n")
		sb.WriteString("|---|------|--------|\n")
		for _, item := range gaps {
			fmt.Fprintf(&sb, "| %d | %s | %s |\n", item.ID, item.Title, statusLabel(item.Status))
		}
		sb.WriteString("\n")
	}

	fmt.Fprintf(&sb, "---\n\n## Overall Verdict: **%s**\n\n", e.Verdict)
	if e.Summary != "" {
		sb.WriteString(e.Summary + "\n")
	}

	return sb.String()
}

// statusLabel returns the display string for an ItemStatus.
func statusLabel(s ItemStatus) string {
	switch s {
	case StatusComplete:
		return "✅ Complete"
	case StatusPartial:
		return "⚠️ Partial"
	case StatusMissing:
		return "❌ Missing"
	case StatusNA:
		return "N/A"
	default:
		return string(s)
	}
}
