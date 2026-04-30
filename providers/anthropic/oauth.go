package anthropic

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ClaudeCredentials mirrors the OAuth credential record Claude Code
// writes to ~/.claude/.credentials.json on Linux/Windows after a user
// runs Claude Code's /login command. Reusing this file lets gocode
// authenticate against a Claude Pro/Max subscription without running
// its own OAuth flow.
//
// Note: on macOS Claude Code stores these credentials in the system
// Keychain rather than on disk; LoadClaudeCredentials does not yet read
// the Keychain. macOS users can copy the access token into the
// ANTHROPIC_AUTH_TOKEN environment variable instead.
type ClaudeCredentials struct {
	AccessToken      string
	RefreshToken     string
	ExpiresAt        time.Time
	SubscriptionType string
	Scopes           []string
}

// claudeCredentialsPath returns the path of Claude Code's credential
// file in the user's home directory.
func claudeCredentialsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("gocode: home dir: %w", err)
	}
	return filepath.Join(home, ".claude", ".credentials.json"), nil
}

// LoadClaudeCredentials reads ~/.claude/.credentials.json and returns
// the OAuth credentials Claude Code stored there. Returns an error
// satisfying errors.Is(err, os.ErrNotExist) if the file is absent —
// callers can treat that as "user has not run Claude Code's /login yet".
func LoadClaudeCredentials() (*ClaudeCredentials, error) {
	path, err := claudeCredentialsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var raw struct {
		ClaudeAiOauth struct {
			AccessToken      string   `json:"accessToken"`
			RefreshToken     string   `json:"refreshToken"`
			ExpiresAt        int64    `json:"expiresAt"` // Unix milliseconds
			SubscriptionType string   `json:"subscriptionType"`
			Scopes           []string `json:"scopes"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("gocode: parse %s: %w", path, err)
	}
	if raw.ClaudeAiOauth.AccessToken == "" {
		return nil, errors.New("gocode: ~/.claude/.credentials.json is missing claudeAiOauth.accessToken")
	}

	creds := &ClaudeCredentials{
		AccessToken:      raw.ClaudeAiOauth.AccessToken,
		RefreshToken:     raw.ClaudeAiOauth.RefreshToken,
		SubscriptionType: raw.ClaudeAiOauth.SubscriptionType,
		Scopes:           raw.ClaudeAiOauth.Scopes,
	}
	if raw.ClaudeAiOauth.ExpiresAt > 0 {
		creds.ExpiresAt = time.UnixMilli(raw.ClaudeAiOauth.ExpiresAt)
	}
	return creds, nil
}

// Expired reports whether the access token is past its stored expiry.
// gocode does not yet implement OAuth refresh, so callers should ask
// the user to re-run Claude Code's /login when this returns true.
func (c *ClaudeCredentials) Expired() bool {
	if c.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(c.ExpiresAt)
}
