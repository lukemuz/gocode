package agent

import "testing"

func TestJoinInstructions(t *testing.T) {
	tests := []struct {
		name  string
		parts []string
		want  string
	}{
		{
			name:  "empty",
			parts: nil,
			want:  "",
		},
		{
			name:  "single",
			parts: []string{"hello"},
			want:  "hello",
		},
		{
			name:  "two parts",
			parts: []string{"part one", "part two"},
			want:  "part one\n\npart two",
		},
		{
			name:  "empty parts are dropped",
			parts: []string{"", "  ", "keep", "", "also keep"},
			want:  "keep\n\nalso keep",
		},
		{
			name:  "whitespace-only parts trimmed",
			parts: []string{"\t\n", "content\n\n", "\n"},
			want:  "content",
		},
		{
			name:  "all empty",
			parts: []string{"", " ", "\t"},
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := JoinInstructions(tt.parts...)
			if got != tt.want {
				t.Errorf("JoinInstructions(%q) = %q, want %q", tt.parts, got, tt.want)
			}
		})
	}
}
