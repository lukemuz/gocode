package anthropic

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadClaudeCredentials(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp) // for parity on Windows test runners

	// Missing file: errors.Is should match os.ErrNotExist so callers can
	// fall through to other auth sources.
	if _, err := LoadClaudeCredentials(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist when credentials file is absent, got %v", err)
	}

	dir := filepath.Join(tmp, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Well-formed file: parsed fields should round-trip, including
	// Unix-millis ExpiresAt.
	expiry := time.Now().Add(time.Hour).UnixMilli()
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(`{
        "claudeAiOauth": {
            "accessToken": "sk-ant-oat-abc",
            "refreshToken": "sk-ant-ort-abc",
            "expiresAt": `+itoa(expiry)+`,
            "subscriptionType": "max",
            "scopes": ["user:inference"]
        }
    }`), 0o600); err != nil {
		t.Fatal(err)
	}

	creds, err := LoadClaudeCredentials()
	if err != nil {
		t.Fatalf("LoadClaudeCredentials: %v", err)
	}
	if creds.AccessToken != "sk-ant-oat-abc" {
		t.Errorf("AccessToken = %q", creds.AccessToken)
	}
	if creds.RefreshToken != "sk-ant-ort-abc" {
		t.Errorf("RefreshToken = %q", creds.RefreshToken)
	}
	if creds.SubscriptionType != "max" {
		t.Errorf("SubscriptionType = %q", creds.SubscriptionType)
	}
	if creds.Expired() {
		t.Errorf("creds should not be Expired() yet, ExpiresAt=%v now=%v", creds.ExpiresAt, time.Now())
	}

	// Past expiry: Expired() should report true.
	creds.ExpiresAt = time.Now().Add(-time.Minute)
	if !creds.Expired() {
		t.Errorf("Expired() should be true for past ExpiresAt")
	}

	// Missing accessToken: explicit error.
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(`{"claudeAiOauth":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadClaudeCredentials(); err == nil {
		t.Errorf("expected error when accessToken is empty")
	}
}

// itoa is a tiny local helper so the test file can splice an int64 into
// a JSON literal without pulling strconv in twice.
func itoa(n int64) string {
	var buf [20]byte
	pos := len(buf)
	negative := n < 0
	if negative {
		n = -n
	}
	if n == 0 {
		pos--
		buf[pos] = '0'
	}
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
