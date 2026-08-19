package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// DefaultConfigDir returns the default directory path for config files.
func DefaultConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".config", "tc-analyzer")
}

// DefaultConfigFile returns the default config file path.
func DefaultConfigFile() string {
	return filepath.Join(DefaultConfigDir(), "config.toml")
}

// DefaultDBPath returns the default SQLite database file path.
func DefaultDBPath() string {
	return "data.db"
}

// Config holds the application configuration settings.
type Config struct {
	ClientID        string `mapstructure:"client_id"`
	ClientSecret    string `mapstructure:"client_secret"`
	DBPath          string `mapstructure:"db_path"`
	DefaultInterval int    `mapstructure:"default_interval"`
	DefaultDuration int    `mapstructure:"default_duration"`
}

// Load loads the configuration using Viper from specified path or default locations,
// environment variables (TC_*), and applies default fallback values.
func Load(configPath string) (*Config, error) {
	v := viper.New()

	// 1. Default values
	v.SetDefault("db_path", DefaultDBPath())
	v.SetDefault("default_interval", 10)
	v.SetDefault("default_duration", 0)

	// 2. Config file location
	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		v.AddConfigPath(DefaultConfigDir())
		v.SetConfigName("config")
		v.SetConfigType("toml")
	}

	// 3. Read config file if present
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("config file load error: %w", err)
			}
		}
	}

	// 4. Environment variables
	v.SetEnvPrefix("TC")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// 5. Unmarshal into struct
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("config unmarshal error: %w", err)
	}

	return &cfg, nil
}

// Save writes the given config to a TOML file (with comments preserved)
// and updates the viper instance.
func Save(configPath string, cfg *Config) error {
	path := configPath
	if path == "" {
		path = DefaultConfigFile()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	content := fmt.Sprintf(`# TwitCasting Data Analyzer 設定ファイル

# TwitCasting Developer Console で取得した認証情報
client_id     = %q
client_secret = %q

# データ保存先 SQLite DB パス
db_path = %q

# デフォルトのポーリング間隔（秒）
default_interval = %d

# デフォルトの監視時間（秒）。0 = 手動停止まで無限継続
default_duration = %d
`, cfg.ClientID, cfg.ClientSecret, cfg.DBPath, cfg.DefaultInterval, cfg.DefaultDuration)

	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return err
	}

	return nil
}

// IsConfigured checks if essential API credentials are set.
func (c *Config) IsConfigured() bool {
	return c != nil && c.ClientID != "" && c.ClientSecret != ""
}
