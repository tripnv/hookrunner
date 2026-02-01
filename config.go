package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type WorkflowConfig struct {
	Trigger string `yaml:"trigger"`
	Command string `yaml:"command"`
	Workdir string `yaml:"workdir"`
	Timeout int    `yaml:"timeout"`
}

type FunnelConfig struct {
	Enabled bool   `yaml:"enabled"`
	URL     string `yaml:"url"`
}

type DaemonConfig struct {
	PIDFile string `yaml:"pid_file"`
	LogFile string `yaml:"log_file"`
}

type Config struct {
	WebhookSecret string                    `yaml:"webhook_secret"`
	Port          int                       `yaml:"port"`
	Funnel        FunnelConfig              `yaml:"funnel"`
	Daemon        DaemonConfig              `yaml:"daemon"`
	Workflows     map[string]WorkflowConfig `yaml:"workflows"`
}

func defaultConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".hookrunner", "config.yaml")
}

func expandTilde(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(expandTilde(path))
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	applyDefaults(cfg)

	if err := validateConfig(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.Port == 0 {
		cfg.Port = 7890
	}
	if cfg.Daemon.PIDFile == "" {
		cfg.Daemon.PIDFile = "~/.hookrunner/hookrunner.pid"
	}
	if cfg.Daemon.LogFile == "" {
		cfg.Daemon.LogFile = "~/.hookrunner/hookrunner.log"
	}
	for name, wf := range cfg.Workflows {
		if wf.Timeout == 0 {
			wf.Timeout = 300
		}
		cfg.Workflows[name] = wf
	}
}

func validateConfig(cfg *Config) error {
	if cfg.WebhookSecret == "" {
		return fmt.Errorf("webhook_secret is required")
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	for name, wf := range cfg.Workflows {
		if wf.Trigger == "" {
			return fmt.Errorf("workflow %q: trigger is required", name)
		}
		if wf.Command == "" {
			return fmt.Errorf("workflow %q: command is required", name)
		}
	}
	return nil
}

const defaultConfigYAML = `webhook_secret: "changeme"
port: 7890
funnel:
  enabled: true
  url: ""
daemon:
  pid_file: "~/.hookrunner/hookrunner.pid"
  log_file: "~/.hookrunner/hookrunner.log"
workflows:
  claude-review:
    trigger: '/cc\s+@claude'
    command: 'claude -p "Review PR #{{.PRNumber}} in {{.RepoFullName}}"'
    workdir: "~/repos/{{.RepoFullName}}"
    timeout: 300
`

func generateDefaultConfig(path string) error {
	path = expandTilde(path)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}

	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("config already exists at %s", path)
	}

	return os.WriteFile(path, []byte(defaultConfigYAML), 0600)
}
