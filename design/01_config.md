# 設計書 01 — 設定管理 (`internal/config`)

## 1. 概要

Python 版 `config.py` に相当する設定管理モジュール。
Go 版では、デファクトスタンダードである `github.com/spf13/viper` を使用し、設定ファイル、環境変数、CLI フラグ、およびデフォルト値を統一的に管理する。

**優先順位（高い順）**: CLI フラグ > 環境変数 > 設定ファイル > デフォルト値

---

## 2. 設定ファイル

| 項目 | 値 |
|---|---|
| デフォルト設定ディレクトリ | `~/.config/tc-analyzer/` |
| 設定ファイル名 | `config.toml` |
| DB ファイル名 | `data.db` |

### config.toml フォーマット（Python 版互換）

```toml
# TwitCasting Data Collector 設定ファイル

# TwitCasting Developer Console で取得した認証情報
client_id     = "YOUR_CLIENT_ID"
client_secret = "YOUR_CLIENT_SECRET"

# データ保存先 SQLite DB パス
db_path = "/home/user/.config/tc-analyzer/data.db"

# デフォルトのポーリング間隔（秒）
default_interval = 10

# デフォルトの監視時間（秒）。0 = 手動停止まで無限継続
default_duration = 0
```

---

## 3. 環境変数

Viper の `AutomaticEnv` を使用し、自動で環境変数をマッピングする。接頭辞として `TC_` を付与し、アンダースコアで接続する。

| 環境変数名 | マップ先キー (Viper) |
|---|---|
| `TC_CLIENT_ID` | `client_id` |
| `TC_CLIENT_SECRET` | `client_secret` |
| `TC_DB_PATH` | `db_path` |

---

## 4. Go 構造体および関数設計

```go
package config

import (
    "os"
    "path/filepath"
    "strings"

    "github.com/spf13/viper"
)

// デフォルトパス
func DefaultConfigDir() string {
    home, _ := os.UserHomeDir()
    return filepath.Join(home, ".config", "tc-analyzer")
}

func DefaultConfigFile() string {
    return filepath.Join(DefaultConfigDir(), "config.toml")
}

func DefaultDBPath() string {
    return filepath.Join(DefaultConfigDir(), "data.db")
}

// Config は設定値を保持する構造体 (Viperからマッピングする)
type Config struct {
    ClientID        string `mapstructure:"client_id"`
    ClientSecret    string `mapstructure:"client_secret"`
    DBPath          string `mapstructure:"db_path"`
    DefaultInterval int    `mapstructure:"default_interval"`
    DefaultDuration int    `mapstructure:"default_duration"`
}

// Load は Viper を用いて設定をロードし、Config 構造体として取得する
func Load(configPath string) (*Config, error)

// Save は設定を TOML ファイルに書き込む (コメント維持のため自前出力する)
func Save(configPath string, cfg *Config) error

// IsConfigured は必須の認証情報が存在するか確認する
func (c *Config) IsConfigured() bool
```

---

## 5. Load() 実装詳細 (Viperの活用)

```go
func Load(configPath string) (*Config, error) {
    // 1. デフォルト値の設定
    viper.SetDefault("db_path", DefaultDBPath())
    viper.SetDefault("default_interval", 10)
    viper.SetDefault("default_duration", 0)

    // 2. 設定ファイルのパス設定
    if configPath != "" {
        viper.SetConfigFile(configPath)
    } else {
        viper.AddConfigPath(DefaultConfigDir())
        viper.SetConfigName("config")
        viper.SetConfigType("toml")
    }

    // 3. 設定ファイルの読み込み (存在しない場合は無視する)
    if err := viper.ReadInConfig(); err != nil {
        if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
            return nil, fmt.Errorf("config file load error: %w", err)
        }
    }

    // 4. 環境変数の設定 (TC_CLIENT_ID などをマッピング)
    viper.SetEnvPrefix("TC")
    viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
    viper.AutomaticEnv()

    // 5. 構造体へのアンマーシャル
    var cfg Config
    if err := viper.Unmarshal(&cfg); err != nil {
        return nil, fmt.Errorf("config unmarshal error: %w", err)
    }

    return &cfg, nil
}
```

---

## 6. Save() 実装詳細

Viper に直接 TOML を書き出させるとコメントやフォーマットが消えてしまうため、Python版互換のカスタムテンプレート文字列を用いて書き出す。書き出し成功後、Viper インスタンスの内部バッファにも同期する。

```go
func Save(configPath string, cfg *Config) error {
    path := configPath
    if path == "" {
        path = DefaultConfigFile()
    }
    if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
        return err
    }
    content := fmt.Sprintf(`# TwitCasting Data Collector 設定ファイル

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

    // Viper の内部バッファも更新
    viper.Set("client_id", cfg.ClientID)
    viper.Set("client_secret", cfg.ClientSecret)
    viper.Set("db_path", cfg.DBPath)
    viper.Set("default_interval", cfg.DefaultInterval)
    viper.Set("default_duration", cfg.DefaultDuration)

    return nil
}
```

---

## 7. Python 版との差分

| 項目 | Python 版 | Go 版 (Viper) |
|---|---|---|
| パーサー | `tomllib` / `tomli` | `github.com/spf13/viper` |
| 環境変数バインド | 自前でマッピング | `viper.AutomaticEnv` による自動結合 |
| フラグ結合 | 手動解決 | Cobra フラグと `viper.BindPFlag` でマージ可能 |
| デフォルト指定 | コード上での分岐 | `viper.SetDefault` による一元管理 |
