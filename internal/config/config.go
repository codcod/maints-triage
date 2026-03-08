package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	JiraURL      string
	JiraUsername string
	JiraAPIToken string
	CursorAPIKey string
}

// Load reads configuration from a .env file (if present) and environment variables.
// Environment variables take precedence over values in the .env file.
func Load() (*Config, error) {
	// Best-effort load of .env; ignore if file doesn't exist
	_ = godotenv.Load()

	cfg := &Config{
		JiraURL:      os.Getenv("JIRA_URL"),
		JiraUsername: os.Getenv("JIRA_USERNAME"),
		JiraAPIToken: os.Getenv("JIRA_API_TOKEN"),
		CursorAPIKey: os.Getenv("CURSOR_API_KEY"),
	}

	return cfg, cfg.validate()
}

func (c *Config) validate() error {
	var missing []string
	if c.JiraURL == "" {
		missing = append(missing, "JIRA_URL")
	}
	if c.JiraUsername == "" {
		missing = append(missing, "JIRA_USERNAME")
	}
	if c.JiraAPIToken == "" {
		missing = append(missing, "JIRA_API_TOKEN")
	}
	if c.CursorAPIKey == "" {
		missing = append(missing, "CURSOR_API_KEY")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variables:\n  %s", strings.Join(missing, "\n  "))
	}
	return nil
}
