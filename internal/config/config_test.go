package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandTilde(t *testing.T) {
	home, _ := os.UserHomeDir()

	tests := []struct {
		input    string
		expected string
	}{
		{"~/foo", filepath.Join(home, "foo")},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
	}

	for _, tt := range tests {
		got := ExpandTilde(tt.input)
		if got != tt.expected {
			t.Errorf("ExpandTilde(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestApplyDefaults(t *testing.T) {
	cfg := &Config{
		Workflows: map[string]WorkflowConfig{
			"test": {Trigger: "foo", Command: "bar"},
		},
	}
	ApplyDefaults(cfg)

	if cfg.Port != 7890 {
		t.Errorf("default port = %d, want 7890", cfg.Port)
	}
	if cfg.Daemon.PIDFile != "~/.hookrunner/hookrunner.pid" {
		t.Errorf("default PID file = %q", cfg.Daemon.PIDFile)
	}
	if cfg.Workflows["test"].Timeout != 300 {
		t.Errorf("default timeout = %d, want 300", cfg.Workflows["test"].Timeout)
	}
}

func TestValidateConfig(t *testing.T) {
	t.Run("missing secret", func(t *testing.T) {
		cfg := &Config{Port: 7890}
		if err := ValidateConfig(cfg); err == nil {
			t.Error("expected error for missing secret")
		}
	})

	t.Run("invalid port", func(t *testing.T) {
		cfg := &Config{WebhookSecret: "s", Port: 0}
		if err := ValidateConfig(cfg); err == nil {
			t.Error("expected error for invalid port")
		}
	})

	t.Run("workflow missing trigger", func(t *testing.T) {
		cfg := &Config{
			WebhookSecret: "s",
			Port:          8080,
			Workflows: map[string]WorkflowConfig{
				"test": {Command: "echo"},
			},
		}
		if err := ValidateConfig(cfg); err == nil {
			t.Error("expected error for missing trigger")
		}
	})

	t.Run("valid config", func(t *testing.T) {
		cfg := &Config{
			WebhookSecret: "s",
			Port:          8080,
			Workflows: map[string]WorkflowConfig{
				"test": {Trigger: "foo", Command: "bar"},
			},
		}
		if err := ValidateConfig(cfg); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	yaml := `
webhook_secret: "testsecret"
port: 9999
workflows:
  test:
    trigger: '/cc'
    command: 'echo hello'
`
	os.WriteFile(path, []byte(yaml), 0600)

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Port != 9999 {
		t.Errorf("port = %d, want 9999", cfg.Port)
	}
	if cfg.WebhookSecret != "testsecret" {
		t.Errorf("secret = %q", cfg.WebhookSecret)
	}
	if cfg.Workflows["test"].Timeout != 300 {
		t.Errorf("timeout = %d, want 300 (default)", cfg.Workflows["test"].Timeout)
	}
}

func TestGenerateDefaultConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config.yaml")

	if err := GenerateDefault(path); err != nil {
		t.Fatal(err)
	}

	// Should fail on second call (already exists)
	if err := GenerateDefault(path); err == nil {
		t.Error("expected error when config exists")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(data) == 0 {
		t.Error("config file is empty")
	}
}
