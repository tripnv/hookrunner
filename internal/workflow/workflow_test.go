package workflow

import (
	"testing"
)

func TestSanitize(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "hello"},
		{"hello; rm -rf /", "hello rm -rf /"},
		{"$(whoami)", "whoami"},
		{"`whoami`", "whoami"},
		{"foo|bar", "foobar"},
		{"a&b", "ab"},
		{"normal-text_123.go", "normal-text_123.go"},
		{"line1\nline2", "line1line2"},
	}

	for _, tt := range tests {
		got := Sanitize(tt.input)
		if got != tt.expected {
			t.Errorf("Sanitize(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestRenderTemplate(t *testing.T) {
	vars := TemplateVars{
		RepoFullName:  "org/repo",
		RepoCloneURL:  "https://github.com/org/repo.git",
		PRNumber:      "42",
		CommentBody:   "please review",
		CommentAuthor: "alice",
		EventType:     "issue_comment",
	}

	t.Run("basic template", func(t *testing.T) {
		result, err := RenderTemplate("echo {{.RepoFullName}} #{{.PRNumber}}", vars)
		if err != nil {
			t.Fatal(err)
		}
		if result != "echo org/repo #42" {
			t.Errorf("got %q", result)
		}
	})

	t.Run("all variables", func(t *testing.T) {
		tmpl := "{{.RepoFullName}} {{.RepoCloneURL}} {{.PRNumber}} {{.CommentBody}} {{.CommentAuthor}} {{.EventType}}"
		result, err := RenderTemplate(tmpl, vars)
		if err != nil {
			t.Fatal(err)
		}
		expected := "org/repo https://github.com/org/repo.git 42 please review alice issue_comment"
		if result != expected {
			t.Errorf("got %q, want %q", result, expected)
		}
	})

	t.Run("invalid template", func(t *testing.T) {
		_, err := RenderTemplate("{{.Invalid", vars)
		if err == nil {
			t.Error("expected error for invalid template")
		}
	})
}

func TestSanitizeVars(t *testing.T) {
	vars := TemplateVars{
		RepoFullName:  "org/repo",
		CommentBody:   "hello; rm -rf /",
		CommentAuthor: "alice$(whoami)",
	}

	safe := SanitizeVars(vars)

	if safe.CommentBody != "hello rm -rf /" {
		t.Errorf("CommentBody not sanitized: %q", safe.CommentBody)
	}
	if safe.CommentAuthor != "alicewhoami" {
		t.Errorf("CommentAuthor not sanitized: %q", safe.CommentAuthor)
	}
	if safe.RepoFullName != "org/repo" {
		t.Errorf("RepoFullName should be unchanged: %q", safe.RepoFullName)
	}
}
