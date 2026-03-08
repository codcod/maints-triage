package triage

import (
	"strings"
	"testing"
)

// --- parseEvaluation ---

func TestParseEvaluation_ValidJSON(t *testing.T) {
	raw := `{
		"items": [
			{"id":1,"title":"Priority","status":"complete","evidence":"Priority: Major","reasoning":"Default priority, no justification required."},
			{"id":2,"title":"Component","status":"missing","evidence":"","reasoning":"No component listed."}
		],
		"summary": "Component is missing.",
		"verdict": "FAIL",
		"review_required": false
	}`

	e, err := parseEvaluation(raw)
	if err != nil {
		t.Fatalf("parseEvaluation() error = %v", err)
	}
	if e == nil {
		t.Fatal("parseEvaluation() returned nil for valid JSON")
	}
	if len(e.Items) != 2 {
		t.Errorf("Items len = %d, want 2", len(e.Items))
	}
	if e.Verdict != "FAIL" {
		t.Errorf("Verdict = %q, want FAIL", e.Verdict)
	}
	if e.Items[0].Status != StatusComplete {
		t.Errorf("Items[0].Status = %q, want complete", e.Items[0].Status)
	}
	if e.Items[1].Status != StatusMissing {
		t.Errorf("Items[1].Status = %q, want missing", e.Items[1].Status)
	}
}

func TestParseEvaluation_JSONInCodeFence(t *testing.T) {
	raw := "Here is the evaluation:\n\n```json\n" +
		`{"items":[{"id":1,"title":"Priority","status":"complete","evidence":"Major","reasoning":"ok"}],"summary":"All good.","verdict":"PASS","review_required":false}` +
		"\n```\n\nDone."

	e, err := parseEvaluation(raw)
	if err != nil {
		t.Fatalf("parseEvaluation() error = %v", err)
	}
	if e == nil {
		t.Fatal("parseEvaluation() returned nil for fenced JSON")
	}
	if e.Verdict != "PASS" {
		t.Errorf("Verdict = %q, want PASS", e.Verdict)
	}
}

func TestParseEvaluation_PlainText_ReturnsNil(t *testing.T) {
	e, err := parseEvaluation("This is a plain text report with no JSON.")
	if err != nil {
		t.Fatalf("parseEvaluation() unexpected error: %v", err)
	}
	if e != nil {
		t.Errorf("expected nil for plain text input, got %+v", e)
	}
}

func TestParseEvaluation_EmptyString_ReturnsNil(t *testing.T) {
	e, err := parseEvaluation("   ")
	if err != nil {
		t.Fatalf("parseEvaluation() unexpected error: %v", err)
	}
	if e != nil {
		t.Errorf("expected nil for empty input, got %+v", e)
	}
}

func TestParseEvaluation_JSONWithNoItems_ReturnsNil(t *testing.T) {
	// Valid JSON but does not look like an Evaluation (no items, no verdict).
	e, err := parseEvaluation(`{"foo":"bar"}`)
	if err != nil {
		t.Fatalf("parseEvaluation() unexpected error: %v", err)
	}
	if e != nil {
		t.Errorf("expected nil for JSON without items/verdict, got %+v", e)
	}
}

func TestParseEvaluation_JSONWithEmptyVerdict_ReturnsNil(t *testing.T) {
	raw := `{"items":[{"id":1,"title":"T","status":"complete","evidence":"e","reasoning":"r"}],"summary":"","verdict":"","review_required":false}`
	e, err := parseEvaluation(raw)
	if err != nil {
		t.Fatalf("parseEvaluation() unexpected error: %v", err)
	}
	if e != nil {
		t.Errorf("expected nil when verdict is empty, got %+v", e)
	}
}

func TestParseEvaluation_AllStatusValues(t *testing.T) {
	raw := `{
		"items": [
			{"id":1,"title":"A","status":"complete","evidence":"q","reasoning":"r"},
			{"id":2,"title":"B","status":"partial","evidence":"q","reasoning":"r"},
			{"id":3,"title":"C","status":"missing","evidence":"","reasoning":"r"},
			{"id":4,"title":"D","status":"na","evidence":"","reasoning":"r"}
		],
		"summary": "two issues",
		"verdict": "FAIL",
		"review_required": false
	}`

	e, err := parseEvaluation(raw)
	if err != nil || e == nil {
		t.Fatalf("parseEvaluation() error = %v, e = %v", err, e)
	}
	want := []ItemStatus{StatusComplete, StatusPartial, StatusMissing, StatusNA}
	for i, w := range want {
		if e.Items[i].Status != w {
			t.Errorf("Items[%d].Status = %q, want %q", i, e.Items[i].Status, w)
		}
	}
}

// --- validateEvaluation ---

func TestValidateEvaluation_Clean(t *testing.T) {
	e := &Evaluation{
		Items: []ChecklistItem{
			{ID: 1, Title: "Priority", Status: StatusComplete, Evidence: "Priority: Major", Reasoning: "ok"},
			{ID: 2, Title: "Component", Status: StatusNA, Evidence: "", Reasoning: "not applicable"},
		},
		Summary: "All good.",
		Verdict: "PASS",
	}
	warnings := validateEvaluation(e)
	if len(warnings) != 0 {
		t.Errorf("expected no warnings for clean evaluation, got: %v", warnings)
	}
}

func TestValidateEvaluation_PassWithMissingItem(t *testing.T) {
	e := &Evaluation{
		Items: []ChecklistItem{
			{ID: 1, Title: "Priority", Status: StatusMissing, Evidence: "", Reasoning: "not present"},
		},
		Verdict: "PASS",
	}
	warnings := validateEvaluation(e)
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "PASS") && strings.Contains(w, "missing") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected warning about PASS with missing items, got: %v", warnings)
	}
}

func TestValidateEvaluation_FailWithAllComplete(t *testing.T) {
	e := &Evaluation{
		Items: []ChecklistItem{
			{ID: 1, Title: "Priority", Status: StatusComplete, Evidence: "e", Reasoning: "r"},
		},
		Verdict: "FAIL",
	}
	warnings := validateEvaluation(e)
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "FAIL") && strings.Contains(w, "complete") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected warning about FAIL with all complete items, got: %v", warnings)
	}
}

func TestValidateEvaluation_MissingEvidenceForNonNA(t *testing.T) {
	e := &Evaluation{
		Items: []ChecklistItem{
			{ID: 1, Title: "Priority", Status: StatusComplete, Evidence: "", Reasoning: "ok"},
		},
		Verdict: "PASS",
	}
	warnings := validateEvaluation(e)
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "evidence") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected warning about missing evidence, got: %v", warnings)
	}
}

func TestValidateEvaluation_UnrecognisedStatus(t *testing.T) {
	e := &Evaluation{
		Items: []ChecklistItem{
			{ID: 1, Title: "Priority", Status: "unknown", Evidence: "e", Reasoning: "r"},
		},
		Verdict: "FAIL",
	}
	warnings := validateEvaluation(e)
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "unrecognised status") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected warning about unrecognised status, got: %v", warnings)
	}
}

func TestValidateEvaluation_UnrecognisedVerdict(t *testing.T) {
	e := &Evaluation{
		Items:   []ChecklistItem{{ID: 1, Title: "T", Status: StatusComplete, Evidence: "e", Reasoning: "r"}},
		Verdict: "MAYBE",
	}
	warnings := validateEvaluation(e)
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "unrecognised verdict") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected warning about unrecognised verdict, got: %v", warnings)
	}
}

// --- renderEvaluationMarkdown ---

func TestRenderEvaluationMarkdown_Structure(t *testing.T) {
	e := &Evaluation{
		Items: []ChecklistItem{
			{ID: 1, Title: "Priority", Status: StatusComplete, Evidence: "Priority is Major.", Reasoning: "Default priority."},
			{ID: 2, Title: "Component", Status: StatusMissing, Evidence: "", Reasoning: "Nothing listed."},
			{ID: 3, Title: "Environment", Status: StatusNA, Evidence: "", Reasoning: "N/A for this issue type."},
		},
		Summary: "Component is missing.",
		Verdict: "FAIL",
	}

	out := renderEvaluationMarkdown(e)

	checks := []string{
		"## Checklist Evaluation",
		"### 1. Priority — ✅ Complete",
		"> Priority is Major.",
		"Default priority.",
		"### 2. Component — ❌ Missing",
		"Nothing listed.",
		"### 3. Environment — N/A",
		"## Summary of Gaps",
		"| 2 | Component | ❌ Missing |",
		"## Overall Verdict: **FAIL**",
		"Component is missing.",
	}
	for _, check := range checks {
		if !strings.Contains(out, check) {
			t.Errorf("expected output to contain %q\nfull output:\n%s", check, out)
		}
	}
}

func TestRenderEvaluationMarkdown_NoGaps_NoGapTable(t *testing.T) {
	e := &Evaluation{
		Items: []ChecklistItem{
			{ID: 1, Title: "Priority", Status: StatusComplete, Evidence: "Major", Reasoning: "ok"},
		},
		Summary: "All good.",
		Verdict: "PASS",
	}

	out := renderEvaluationMarkdown(e)

	if strings.Contains(out, "Summary of Gaps") {
		t.Error("expected no gap table when all items are complete")
	}
	if !strings.Contains(out, "## Overall Verdict: **PASS**") {
		t.Error("expected PASS verdict in output")
	}
}

func TestRenderEvaluationMarkdown_PartialInGapTable(t *testing.T) {
	e := &Evaluation{
		Items: []ChecklistItem{
			{ID: 7, Title: "Expected vs Actual", Status: StatusPartial, Evidence: "Actual described.", Reasoning: "Expected not stated."},
		},
		Summary: "Expected behavior missing.",
		Verdict: "FAIL",
	}

	out := renderEvaluationMarkdown(e)

	if !strings.Contains(out, "⚠️ Partial") {
		t.Error("expected partial status label in output")
	}
	if !strings.Contains(out, "| 7 | Expected vs Actual | ⚠️ Partial |") {
		t.Errorf("expected partial item in gap table\nfull output:\n%s", out)
	}
}

// --- extractJSONBlock ---

func TestExtractJSONBlock(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no fence",
			input: "plain text",
			want:  "",
		},
		{
			name:  "fenced block",
			input: "before\n```json\n{\"key\":\"val\"}\n```\nafter",
			want:  `{"key":"val"}`,
		},
		{
			name:  "unclosed fence returns empty",
			input: "```json\n{\"key\":\"val\"}",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSONBlock(tt.input)
			if got != tt.want {
				t.Errorf("extractJSONBlock() = %q, want %q", got, tt.want)
			}
		})
	}
}
