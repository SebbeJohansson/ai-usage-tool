// Package settings turns process environment variables into a typed config
// struct. A .env file in the working directory is picked up automatically if
// present; nothing breaks if it's absent, since real deployments (Docker)
// inject the environment directly.
package settings

import (
	"os"
	"time"

	"github.com/joho/godotenv"
)

// Settings is every knob this program reads. Fields are left empty when the
// matching env var is unset — callers that need a value decide for
// themselves whether an empty field means "skip this feature."
type Settings struct {
	AnthropicOrg     string
	AnthropicSession string

	CopilotOrg   string
	CopilotToken string
	WatchedLogin string

	DiscordHook string

	PollEvery  time.Duration
	LedgerPath string
}

const (
	defaultPollEvery  = 15 * time.Minute
	defaultLedgerPath = "data/state.json"
)

// FromEnv builds a Settings from the process environment, loading a local
// .env first when one exists.
func FromEnv() (Settings, error) {
	_ = godotenv.Load()

	s := Settings{
		AnthropicOrg:     os.Getenv("CLAUDE_ORG_ID"),
		AnthropicSession: os.Getenv("CLAUDE_SESSION_KEY"),
		CopilotOrg:       os.Getenv("GITHUB_ORG"),
		CopilotToken:     os.Getenv("GITHUB_TOKEN"),
		WatchedLogin:     os.Getenv("GITHUB_USER"),
		DiscordHook:      os.Getenv("DISCORD_WEBHOOK_URL"),
		PollEvery:        defaultPollEvery,
		LedgerPath:       defaultLedgerPath,
	}

	if raw := os.Getenv("CHECK_INTERVAL"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return Settings{}, err
		}
		s.PollEvery = d
	}

	if raw := os.Getenv("STATE_FILE"); raw != "" {
		s.LedgerPath = raw
	}

	return s, nil
}

// HasClaude reports whether enough is configured to poll Claude.
func (s Settings) HasClaude() bool {
	return s.AnthropicOrg != "" && s.AnthropicSession != ""
}

// HasCopilot reports whether enough is configured to poll GitHub Copilot.
func (s Settings) HasCopilot() bool {
	return s.CopilotOrg != "" && s.CopilotToken != "" && s.WatchedLogin != ""
}
