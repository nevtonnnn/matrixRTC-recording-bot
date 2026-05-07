package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Matrix    MatrixConfig    `yaml:"matrix"`
	LiveKit   LiveKitConfig   `yaml:"livekit"`
	Recording RecordingConfig `yaml:"recording"`
	Webhook   WebhookConfig   `yaml:"webhook"`
	Layouts   LayoutsConfig   `yaml:"layouts"`
}

type MatrixConfig struct {
	Homeserver  string `yaml:"homeserver"`
	UserID      string `yaml:"user_id"`
	AccessToken string `yaml:"access_token"`
}

type LiveKitConfig struct {
	URL           string `yaml:"url"`
	APIKey        string `yaml:"api_key"`
	APISecret     string `yaml:"api_secret"`
	JWTServiceURL string `yaml:"jwt_service_url"`
}

type RecordingConfig struct {
	OutputDir   string `yaml:"output_dir"`
	DefaultMode string `yaml:"default_mode"`
}

type WebhookConfig struct {
	Listen string `yaml:"listen"`
	Path   string `yaml:"path"`
}

type LayoutsConfig struct {
	Listen string `yaml:"listen"`
	Path   string `yaml:"path"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	if cfg.Recording.DefaultMode == "" {
		cfg.Recording.DefaultMode = "screen"
	}
	if cfg.Webhook.Listen == "" {
		cfg.Webhook.Listen = ":8080"
	}
	if cfg.Webhook.Path == "" {
		cfg.Webhook.Path = "/webhook"
	}
	if cfg.Layouts.Listen == "" {
		cfg.Layouts.Listen = ":8080"
	}
	if cfg.Layouts.Path == "" {
		cfg.Layouts.Path = "/layouts/"
	}
	return &cfg, nil
}
