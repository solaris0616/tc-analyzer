package config

import (
	"path/filepath"
	"testing"
)

func TestConfigLoadAndSave(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")

	cfg := &Config{
		ClientID:        "test_client_id",
		ClientSecret:    "test_client_secret",
		DBPath:          filepath.Join(tempDir, "test.db"),
		DefaultInterval: 15,
		DefaultDuration: 60,
	}

	if cfg.IsConfigured() == false {
		t.Errorf("expected IsConfigured to be true")
	}

	if err := Save(configPath, cfg); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if loaded.ClientID != cfg.ClientID {
		t.Errorf("expected ClientID %s, got %s", cfg.ClientID, loaded.ClientID)
	}
	if loaded.ClientSecret != cfg.ClientSecret {
		t.Errorf("expected ClientSecret %s, got %s", cfg.ClientSecret, loaded.ClientSecret)
	}
	if loaded.DBPath != cfg.DBPath {
		t.Errorf("expected DBPath %s, got %s", cfg.DBPath, loaded.DBPath)
	}
	if loaded.DefaultInterval != cfg.DefaultInterval {
		t.Errorf("expected DefaultInterval %d, got %d", cfg.DefaultInterval, loaded.DefaultInterval)
	}
	if loaded.DefaultDuration != cfg.DefaultDuration {
		t.Errorf("expected DefaultDuration %d, got %d", cfg.DefaultDuration, loaded.DefaultDuration)
	}
}

func TestUnconfigured(t *testing.T) {
	cfg := &Config{}
	if cfg.IsConfigured() {
		t.Errorf("expected IsConfigured to be false for empty config")
	}
}
