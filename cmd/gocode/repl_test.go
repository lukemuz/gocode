package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestToolInputPreview(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string // substring expected in result
	}{
		{
			"bash command wins over other fields",
			`{"command": "go test ./...", "cwd": "."}`,
			"command=go test ./...",
		},
		{
			"path field for read_file",
			`{"path": "tools/web/web.go"}`,
			"path=tools/web/web.go",
		},
		{
			"url field for web_fetch",
			`{"url": "https://example.com/docs", "max_length": 4000}`,
			"url=https://example.com/docs",
		},
		{
			"newlines collapsed onto one line",
			`{"command": "echo hi\n\necho bye"}`,
			"echo hi echo bye",
		},
		{
			"long values truncated",
			`{"command": "` + strings.Repeat("x", 200) + `"}`,
			"…",
		},
		{
			"empty input returns empty",
			``,
			"",
		},
		{
			"unknown shape falls back to key list",
			`{"weird_key": 42}`,
			"weird_key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toolInputPreview(json.RawMessage(tt.input))
			if tt.want == "" {
				if got != "" {
					t.Errorf("expected empty preview, got %q", got)
				}
				return
			}
			if !strings.Contains(got, tt.want) {
				t.Errorf("preview = %q, want substring %q", got, tt.want)
			}
		})
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{1024 * 1024, "1.0 MiB"},
		{5 * 1024 * 1024, "5.0 MiB"},
	}
	for _, tt := range tests {
		if got := humanBytes(tt.n); got != tt.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}
