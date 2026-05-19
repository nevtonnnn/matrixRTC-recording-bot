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
	Nextcloud NextcloudConfig `yaml:"nextcloud"`
}

type MatrixConfig struct {
	Homeserver string `yaml:"homeserver"`
	UserID     string `yaml:"user_id"`
	Password   string `yaml:"password"`
	PickleKey  string `yaml:"pickle_key"`
}

type LiveKitConfig struct {
	URL           string `yaml:"url"`
	APIKey        string `yaml:"api_key"`
	APISecret     string `yaml:"api_secret"`
	JWTServiceURL string `yaml:"jwt_service_url"`
}

type RecordingConfig struct {
	OutputDir      string `yaml:"output_dir"`
	DefaultMode    string `yaml:"default_mode"`
	MaxVideoHeight int32  `yaml:"max_video_height"`
	MaxConcurrent  int    `yaml:"max_concurrent"`
}

type WebhookConfig struct {
	Listen string `yaml:"listen"`
	Path   string `yaml:"path"`
}

type LayoutsConfig struct {
	Listen  string `yaml:"listen"`
	Path    string `yaml:"path"`
	BaseURL string `yaml:"base_url"`
}

type NextcloudConfig struct {
	URL               string `yaml:"url"`
	Username          string `yaml:"username"`
	Password          string `yaml:"password"`
	UploadDir         string `yaml:"upload_dir"`
	DeleteAfterUpload bool   `yaml:"delete_after_upload"`
	RetentionDays     int    `yaml:"retention_days"`
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
	if cfg.Recording.MaxVideoHeight == 0 {
		cfg.Recording.MaxVideoHeight = 720
	}
	if cfg.Recording.MaxConcurrent == 0 {
		cfg.Recording.MaxConcurrent = 1
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
	if cfg.Nextcloud.UploadDir == "" {
		cfg.Nextcloud.UploadDir = "/Recordings"
	}
	if cfg.Nextcloud.RetentionDays == 0 && cfg.Nextcloud.URL != "" {
		cfg.Nextcloud.RetentionDays = 30
	}
	return &cfg, nil
}
