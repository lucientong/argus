package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config holds all Argus runtime configuration.
type Config struct {
	Server       ServerConfig       `yaml:"server"`
	Integrations IntegrationsConfig `yaml:"integrations"`
	LLM          LLMConfig          `yaml:"llm"`
	Slack        SlackConfig        `yaml:"slack"`
	Prometheus   PrometheusConfig   `yaml:"prometheus"`
	Kubernetes   KubernetesConfig   `yaml:"kubernetes"`
	Grafana      GrafanaConfig      `yaml:"grafana"`
	PagerDuty    PagerDutyConfig    `yaml:"pagerduty"`
	Runbooks     RunbooksConfig     `yaml:"runbooks"`
}

type IntegrationsConfig struct {
	// Mode controls whether real HTTP clients or in-memory mocks are used.
	// Valid values: "mock" (default) | "real"
	Mode string `yaml:"mode"`
}

type ServerConfig struct {
	Port int `yaml:"port"`
}

type LLMConfig struct {
	Provider   string `yaml:"provider"`    // "anthropic" | "openai"
	Model      string `yaml:"model"`
	APIKey     string `yaml:"api_key"`     // falls back to env var
	FallbackProvider string `yaml:"fallback_provider"`
	FallbackModel    string `yaml:"fallback_model"`
	FallbackAPIKey   string `yaml:"fallback_api_key"`
}

type SlackConfig struct {
	Token          string `yaml:"token"`
	AlertsChannel  string `yaml:"alerts_channel"`
	ApprovalChannel string `yaml:"approval_channel"`
}

type PrometheusConfig struct {
	URL string `yaml:"url"`
}

type KubernetesConfig struct {
	KubeconfigPath string `yaml:"kubeconfig_path"`
	Namespace      string `yaml:"namespace"`
}

type GrafanaConfig struct {
	URL    string `yaml:"url"`
	APIKey string `yaml:"api_key"`
	WebhookSecret string `yaml:"webhook_secret"`
}

type PagerDutyConfig struct {
	IntegrationKey string `yaml:"integration_key"`
	WebhookSecret  string `yaml:"webhook_secret"`
}

type RunbooksConfig struct {
	Dir string `yaml:"dir"`
}

// Load reads and parses the config file at path, then resolves env-var overrides.
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("config: open %s: %w", path, err)
	}
	defer f.Close()

	var cfg Config
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("config: decode %s: %w", path, err)
	}

	// Env-var overrides (allow secrets to stay out of config files).
	cfg.LLM.APIKey = envOr("ARGUS_LLM_API_KEY", cfg.LLM.APIKey)
	cfg.LLM.FallbackAPIKey = envOr("ARGUS_LLM_FALLBACK_API_KEY", cfg.LLM.FallbackAPIKey)
	cfg.Slack.Token = envOr("ARGUS_SLACK_TOKEN", cfg.Slack.Token)
	cfg.Grafana.APIKey = envOr("ARGUS_GRAFANA_API_KEY", cfg.Grafana.APIKey)
	cfg.Grafana.WebhookSecret = envOr("ARGUS_GRAFANA_WEBHOOK_SECRET", cfg.Grafana.WebhookSecret)
	cfg.PagerDuty.WebhookSecret = envOr("ARGUS_PAGERDUTY_WEBHOOK_SECRET", cfg.PagerDuty.WebhookSecret)

	// Defaults.
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Integrations.Mode == "" {
		cfg.Integrations.Mode = "mock"
	}
	if cfg.Runbooks.Dir == "" {
		cfg.Runbooks.Dir = "runbooks"
	}

	return &cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
