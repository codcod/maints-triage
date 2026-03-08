package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/codcod/maints-triage/internal/config"
	"github.com/codcod/maints-triage/internal/triage"
)

// version is set at build time via -ldflags="-X main.version=<tag>".
var version = "dev"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var (
		checklistPath string
		promptPath    string
		model         string
		outputFormat  string
	)

	cmd := &cobra.Command{
		Use:     "triage <ISSUE-KEY> [ISSUE-KEY...]",
		Short:   "Triage Jira maintenance issues using cursor-agent",
		Version: version,
		Long: `triage fetches Jira maintenance issues and runs cursor-agent to verify
completeness against a configurable checklist.

Required environment variables (or .env file):
  JIRA_URL         Base URL of your Jira instance (e.g. https://acme.atlassian.net)
  JIRA_USERNAME    Jira account email
  JIRA_API_TOKEN   Jira API token
  CURSOR_API_KEY   cursor-agent API key

Optional environment variables:
  TRIAGE_HOME      Directory for triage configuration files
                   (default: $XDG_CONFIG_HOME/triage, or ~/.config/triage)`,
		Example: `  triage PROJ-123
  triage PROJ-123 PROJ-456
  triage --checklist ./custom-checklist.md PROJ-123
  triage --prompt ./custom-prompt.md PROJ-123
  triage --model sonnet-4 --output json PROJ-123`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			return triage.Run(args, cfg, triage.Options{
				ChecklistPath: checklistPath,
				PromptPath:    promptPath,
				Model:         model,
				OutputFormat:  outputFormat,
			}, os.Stdout)
		},
	}

	cmd.Flags().StringVarP(&checklistPath, "checklist", "c", "",
		`path to the checklist Markdown file (default: "./checklist.md")`)
	cmd.Flags().StringVarP(&promptPath, "prompt", "p", "",
		`path to the prompt template Markdown file (default: "./triage-prompt.md")`)
	cmd.Flags().StringVar(&model, "model", "",
		"cursor-agent model to use (e.g. sonnet-4, gpt-5)")
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "text",
		`output format: text | json`)

	return cmd
}
