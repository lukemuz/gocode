package skills_test

import (
	"testing"

	"github.com/lukemuz/gocode/agent/skills"
)

// ---- helpers ----------------------------------------------------------------

// assertSkill verifies the contract every Skill must satisfy:
// non-empty meta fields, non-empty instructions, non-nil toolset with bindings.
func assertSkill(t *testing.T, s skills.Skill, expectedName string, expectedTools []string) {
	t.Helper()

	meta := s.Meta()
	if meta.Name == "" {
		t.Error("Meta().Name is empty")
	}
	if meta.Name != expectedName {
		t.Errorf("Meta().Name = %q, want %q", meta.Name, expectedName)
	}
	if meta.Description == "" {
		t.Error("Meta().Description is empty")
	}
	if meta.Version == "" {
		t.Error("Meta().Version is empty")
	}

	if s.Instructions() == "" {
		t.Error("Instructions() returned empty string")
	}

	ts := s.Toolset()
	if len(ts.Bindings) == 0 {
		t.Error("Toolset().Bindings is empty")
	}

	// Verify dispatch map is consistent with bindings.
	dispatch := ts.Dispatch()
	for _, b := range ts.Bindings {
		if _, ok := dispatch[b.Tool.Name]; !ok {
			t.Errorf("Dispatch() missing tool %q", b.Tool.Name)
		}
		if b.Func == nil {
			t.Errorf("binding %q has nil Func", b.Tool.Name)
		}
	}

	// Verify expected tools are present.
	toolSet := make(map[string]bool, len(ts.Bindings))
	for _, b := range ts.Bindings {
		toolSet[b.Tool.Name] = true
	}
	for _, name := range expectedTools {
		if !toolSet[name] {
			t.Errorf("expected tool %q not found in toolset", name)
		}
	}
}

var workspaceTools = []string{
	"list_directory",
	"find_files",
	"search_text",
	"read_file",
	"file_info",
}

// ---- RepoExplainer ----------------------------------------------------------

func TestNewRepoExplainer_EmptyRoot(t *testing.T) {
	_, err := skills.NewRepoExplainer(skills.RepoExplainerConfig{Root: ""})
	if err == nil {
		t.Fatal("expected error for empty Root, got nil")
	}
}

func TestNewRepoExplainer_ValidRoot(t *testing.T) {
	s, err := skills.NewRepoExplainer(skills.RepoExplainerConfig{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("NewRepoExplainer: %v", err)
	}
	assertSkill(t, s, "repo_explainer", workspaceTools)
}

func TestRepoExplainer_Examples(t *testing.T) {
	s, err := skills.NewRepoExplainer(skills.RepoExplainerConfig{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if ex := s.Examples(); ex != nil {
		t.Errorf("Examples() = %v, want nil", ex)
	}
}

// ---- LogSummarizer ----------------------------------------------------------

func TestNewLogSummarizer_EmptyRoot(t *testing.T) {
	_, err := skills.NewLogSummarizer(skills.LogSummarizerConfig{Root: ""})
	if err == nil {
		t.Fatal("expected error for empty Root, got nil")
	}
}

func TestNewLogSummarizer_ValidRoot(t *testing.T) {
	s, err := skills.NewLogSummarizer(skills.LogSummarizerConfig{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("NewLogSummarizer: %v", err)
	}
	assertSkill(t, s, "log_summarizer", workspaceTools)
}

func TestLogSummarizer_Examples(t *testing.T) {
	s, err := skills.NewLogSummarizer(skills.LogSummarizerConfig{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if ex := s.Examples(); ex != nil {
		t.Errorf("Examples() = %v, want nil", ex)
	}
}

// ---- CodeReview -------------------------------------------------------------

func TestNewCodeReview_EmptyRoot(t *testing.T) {
	_, err := skills.NewCodeReview(skills.CodeReviewConfig{Root: ""})
	if err == nil {
		t.Fatal("expected error for empty Root, got nil")
	}
}

func TestNewCodeReview_ValidRoot(t *testing.T) {
	s, err := skills.NewCodeReview(skills.CodeReviewConfig{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("NewCodeReview: %v", err)
	}
	assertSkill(t, s, "code_review", workspaceTools)
}

func TestCodeReview_Examples(t *testing.T) {
	s, err := skills.NewCodeReview(skills.CodeReviewConfig{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if ex := s.Examples(); ex != nil {
		t.Errorf("Examples() = %v, want nil", ex)
	}
}

// ---- DocsQA -----------------------------------------------------------------

func TestNewDocsQA_EmptyRoot(t *testing.T) {
	_, err := skills.NewDocsQA(skills.DocsQAConfig{Root: ""})
	if err == nil {
		t.Fatal("expected error for empty Root, got nil")
	}
}

func TestNewDocsQA_ValidRoot(t *testing.T) {
	s, err := skills.NewDocsQA(skills.DocsQAConfig{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("NewDocsQA: %v", err)
	}
	assertSkill(t, s, "docs_qa", workspaceTools)
}

func TestDocsQA_Examples(t *testing.T) {
	s, err := skills.NewDocsQA(skills.DocsQAConfig{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if ex := s.Examples(); ex != nil {
		t.Errorf("Examples() = %v, want nil", ex)
	}
}

// ---- Skill interface satisfaction -------------------------------------------

// Compile-time checks that all concrete types satisfy the Skill interface.
var (
	_ skills.Skill = (*skills.RepoExplainer)(nil)
	_ skills.Skill = (*skills.LogSummarizer)(nil)
	_ skills.Skill = (*skills.CodeReview)(nil)
	_ skills.Skill = (*skills.DocsQA)(nil)
)
