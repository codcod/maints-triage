package triage

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codcod/maints-triage/internal/jira"
)

func TestTriageHome(t *testing.T) {
	t.Run("TRIAGE_HOME used when set", func(t *testing.T) {
		t.Setenv("TRIAGE_HOME", "/custom/triage")
		got, err := triageHome()
		if err != nil {
			t.Fatalf("triageHome() error = %v", err)
		}
		if got != "/custom/triage" {
			t.Errorf("got %q, want %q", got, "/custom/triage")
		}
	})

	t.Run("falls back to XDG_CONFIG_HOME/triage", func(t *testing.T) {
		t.Setenv("TRIAGE_HOME", "")
		t.Setenv("XDG_CONFIG_HOME", "/xdg/config")
		got, err := triageHome()
		if err != nil {
			t.Fatalf("triageHome() error = %v", err)
		}
		want := filepath.Join("/xdg/config", "triage")
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

func TestResolveChecklist(t *testing.T) {
	t.Run("explicit path returned as-is", func(t *testing.T) {
		got, err := resolveChecklist("/some/explicit/checklist.md")
		if err != nil {
			t.Fatalf("resolveChecklist() error = %v", err)
		}
		if got != "/some/explicit/checklist.md" {
			t.Errorf("got %q, want %q", got, "/some/explicit/checklist.md")
		}
	})

	t.Run("TRIAGE_HOME used when set and file exists", func(t *testing.T) {
		tmp := t.TempDir()
		checklistPath := filepath.Join(tmp, "checklist.md")
		if err := os.WriteFile(checklistPath, []byte("# TRIAGE_HOME checklist"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("TRIAGE_HOME", tmp)

		got, err := resolveChecklist("")
		if err != nil {
			t.Fatalf("resolveChecklist() error = %v", err)
		}
		if got != checklistPath {
			t.Errorf("got %q, want %q", got, checklistPath)
		}
	})

	t.Run("TRIAGE_HOME takes priority over XDG_CONFIG_HOME", func(t *testing.T) {
		tmp := t.TempDir()
		triageHomeDir := filepath.Join(tmp, "triage-home")
		if err := os.MkdirAll(triageHomeDir, 0o755); err != nil {
			t.Fatal(err)
		}
		triageChecklist := filepath.Join(triageHomeDir, "checklist.md")
		if err := os.WriteFile(triageChecklist, []byte("# TRIAGE_HOME"), 0o644); err != nil {
			t.Fatal(err)
		}

		xdgDir := filepath.Join(tmp, "xdg", "triage")
		if err := os.MkdirAll(xdgDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(xdgDir, "checklist.md"), []byte("# XDG"), 0o644); err != nil {
			t.Fatal(err)
		}

		t.Setenv("TRIAGE_HOME", triageHomeDir)
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "xdg"))

		got, err := resolveChecklist("")
		if err != nil {
			t.Fatalf("resolveChecklist() error = %v", err)
		}
		if got != triageChecklist {
			t.Errorf("got %q, want %q", got, triageChecklist)
		}
	})

	t.Run("XDG path used when file exists and TRIAGE_HOME is unset", func(t *testing.T) {
		tmp := t.TempDir()
		xdgDir := filepath.Join(tmp, "triage")
		if err := os.MkdirAll(xdgDir, 0o755); err != nil {
			t.Fatal(err)
		}
		checklistPath := filepath.Join(xdgDir, "checklist.md")
		if err := os.WriteFile(checklistPath, []byte("# XDG checklist"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("TRIAGE_HOME", "")
		t.Setenv("XDG_CONFIG_HOME", tmp)

		got, err := resolveChecklist("")
		if err != nil {
			t.Fatalf("resolveChecklist() error = %v", err)
		}
		if got != checklistPath {
			t.Errorf("got %q, want %q", got, checklistPath)
		}
	})

	t.Run("falls back to ./checklist.md when no config file found", func(t *testing.T) {
		t.Setenv("TRIAGE_HOME", "")
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())

		got, err := resolveChecklist("")
		if err != nil {
			t.Fatalf("resolveChecklist() error = %v", err)
		}
		if got != "checklist.md" {
			t.Errorf("got %q, want %q", got, "checklist.md")
		}
	})
}

func TestBuildPrompt(t *testing.T) {
	key := "MAINT-42"
	prompt := buildPrompt(key)

	if !strings.Contains(prompt, "issue-MAINT-42.md") {
		t.Errorf("prompt should reference the issue file, got: %s", prompt)
	}
	if !strings.Contains(prompt, "checklist.md") {
		t.Errorf("prompt should reference checklist.md, got: %s", prompt)
	}
	if !strings.Contains(prompt, "✅") {
		t.Errorf("prompt should contain ✅ status marker")
	}
	if !strings.Contains(prompt, "❌") {
		t.Errorf("prompt should contain ❌ status marker")
	}
	if !strings.Contains(prompt, "PASS") || !strings.Contains(prompt, "FAIL") {
		t.Errorf("prompt should contain PASS/FAIL verdict labels")
	}
}

func TestWriteIssueMarkdown(t *testing.T) {
	tmp := t.TempDir()
	issue := &jira.Issue{
		Key:              "MAINT-99",
		Summary:          "Fix the broken thing",
		Status:           "In Progress",
		Priority:         "High",
		Reporter:         "Alice",
		Assignee:         "Bob",
		Components:       []string{"Backend", "Frontend"},
		AffectedVersions: []string{"2.0", "2.1"},
		FixVersions:      []string{"2.2"},
		Labels:           []string{"bug", "regression"},
		ExtraFields:      []jira.FieldValue{{Field: "Customers", Value: "Acme Corp"}},
		Description:      "This is the description.",
		Comments: []jira.Comment{
			{Author: "Charlie", Created: "2024-01-01T10:00:00Z", Body: "Please fix ASAP."},
		},
	}

	path := filepath.Join(tmp, "issue-MAINT-99.md")
	if err := writeIssueMarkdown(path, issue); err != nil {
		t.Fatalf("writeIssueMarkdown() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	checks := []string{
		"# Jira Issue: MAINT-99",
		"Fix the broken thing",
		"In Progress",
		"High",
		"Alice",
		"Bob",
		"Backend, Frontend",
		"Acme Corp",
		"2.0, 2.1",
		"2.2",
		"bug, regression",
		"This is the description.",
		"### Charlie (2024-01-01T10:00:00Z)",
		"Please fix ASAP.",
	}
	for _, check := range checks {
		if !strings.Contains(content, check) {
			t.Errorf("expected output to contain %q\nfull output:\n%s", check, content)
		}
	}
}

func TestWriteIssueMarkdown_NoComments(t *testing.T) {
	tmp := t.TempDir()
	issue := &jira.Issue{
		Key:     "MAINT-1",
		Summary: "No comments here",
	}

	path := filepath.Join(tmp, "issue-MAINT-1.md")
	if err := writeIssueMarkdown(path, issue); err != nil {
		t.Fatalf("writeIssueMarkdown() error = %v", err)
	}

	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "## Comments") {
		t.Error("expected no Comments section when there are no comments")
	}
}

func TestWriteReport(t *testing.T) {
	tmp := t.TempDir()
	r := Result{
		IssueKey:  "MAINT-55",
		Summary:   "Some maintenance issue",
		TriagedAt: time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC),
		Report:    "✅ Everything looks good.",
	}

	path := filepath.Join(tmp, "report-MAINT-55.md")
	if err := writeReport(path, r); err != nil {
		t.Fatalf("writeReport() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, "# Triage Report: MAINT-55") {
		t.Error("expected report header with issue key")
	}
	if !strings.Contains(content, "Some maintenance issue") {
		t.Error("expected summary in report")
	}
	if !strings.Contains(content, "2024-06-01T12:00:00Z") {
		t.Error("expected triaged-at timestamp in report")
	}
	if !strings.Contains(content, "✅ Everything looks good.") {
		t.Error("expected report body content")
	}
}

func TestPrintResult_TextFormat(t *testing.T) {
	var buf bytes.Buffer
	r := Result{
		IssueKey:  "MAINT-1",
		Summary:   "My summary",
		TriagedAt: time.Now(),
		Report:    "All good.",
	}
	printResult(&buf, r, "text")
	out := buf.String()

	if !strings.Contains(out, "MAINT-1") {
		t.Error("output should contain issue key")
	}
	if !strings.Contains(out, "My summary") {
		t.Error("output should contain summary")
	}
	if !strings.Contains(out, "All good.") {
		t.Error("output should contain report content")
	}
}

func TestPrintResult_JSONFormat(t *testing.T) {
	var buf bytes.Buffer
	r := Result{
		IssueKey: "MAINT-2",
		Summary:  "JSON test",
		Report:   "Looks good.",
	}
	printResult(&buf, r, "json")

	var decoded Result
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, buf.String())
	}
	if decoded.IssueKey != "MAINT-2" {
		t.Errorf("IssueKey = %q, want %q", decoded.IssueKey, "MAINT-2")
	}
	if decoded.Summary != "JSON test" {
		t.Errorf("Summary = %q, want %q", decoded.Summary, "JSON test")
	}
}

func TestPrintResult_TextFormatWithError(t *testing.T) {
	var buf bytes.Buffer
	r := Result{
		IssueKey: "MAINT-3",
		Error:    "something went wrong",
	}
	printResult(&buf, r, "text")
	out := buf.String()

	if !strings.Contains(out, "ERROR: something went wrong") {
		t.Errorf("expected ERROR prefix in output, got: %s", out)
	}
	if strings.Contains(out, "\n\n\n") {
		t.Error("expected no extra blank lines after error")
	}
}
