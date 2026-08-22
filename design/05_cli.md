# 設計書 05 — CLI コマンド定義 (`internal/cli`)

## 1. 概要

`github.com/spf13/cobra`を使用したCLIインターフェース。
`github.com/spf13/cobra`パッケージを使用して、コマンドツリーの構築、フラグの解析、ヘルプメッセージの出力、およびOSシグナルのハンドリングを行う。

---

## 2. コマンドツリー構成

Cobra によるコマンド構成は以下の通りとなる。

```
tc-analyzer (rootCmd)
  ├── watch
  ├── auto-watch
  ├── current
  ├── info
  ├── sessions
  ├── summary
  ├── export
  ├── dashboard
  └── config (configCmd)
        ├── set
        ├── show
        └── verify
```

---

## 3. シグナルハンドリングと Context

Cobra の実行開始時に OS の `SIGINT` (Ctrl+C) や `SIGTERM` を受信するための `context.Context` を作成し、サブコマンドに伝播させる。

### 実装方針

```go
package cli

import (
    "context"
    "os"
    "os/signal"
    "syscall"

    "github.com/spf13/cobra"
)

func Execute() {
    // OSシグナルを監視するキャンセル可能なContextを作成
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer stop()

    rootCmd := NewRootCmd()
    if err := rootCmd.ExecuteContext(ctx); err != nil {
        // エラーはすでに SilenceErrors=true でハンドリングされ出力されている前提
        os.Exit(1)
    }
}
```

---

## 4. 各コマンドの設計

### 4.1. ルートコマンド (`rootCmd`)

- **役割**: グローバルフラグの定義と、設定ファイル初期化処理を `PersistentPreRunE` で一括管理する。また、不要な Usage 出力を抑止する。
- **グローバルフラグ**:
  - `--config <path>`: 設定ファイルパスの指定。

```go
// configKey は context に設定を格納する隞の型キー（外部パッケージとの衝突を避けるため unexported にする）
type configKey struct{}

var configPath string

func NewRootCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "tc-analyzer",
        Short: "TwitCasting 配信データ収集 CLI",
        Long:  `TwitCasting.tv の同時視聴者数・コメント数を定期監視して SQLite に保存します。`,
        // エラー時に Usage を出さず、シンプルにエラー内容だけを出すUX
        SilenceUsage:  true,
        SilenceErrors: false,
        PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
            cfg, err := config.Load(configPath)
            if err != nil {
                return fmt.Errorf("設定のロードに失敗しました: %w", err)
            }
            // グローバル変数ではなく context に格納する（並行テスト安全性と保守性を確保）
            ctx := context.WithValue(cmd.Context(), configKey{}, cfg)
            cmd.SetContext(ctx)
            return nil
        },
    }

    cmd.PersistentFlags().StringVar(&configPath, "config", "", "設定ファイルのパス")

    // サブコマンドの追加
    cmd.AddCommand(NewWatchCmd())
    cmd.AddCommand(NewStatusCmd())
    cmd.AddCommand(NewSessionsCmd())
    cmd.AddCommand(NewSummaryCmd())
    cmd.AddCommand(NewDashboardCmd())
    cmd.AddCommand(NewConfigCmd())

    return cmd
}

// configFromContext は context から設定を取り出すヘルパー
func configFromContext(ctx context.Context) *config.Config {
    cfg, _ := ctx.Value(configKey{}).(*config.Config)
    return cfg
}
```

### 4.2. `watch` コマンド

- **引数**: `movie_id` (必須)
- **フラグ**:
  - `-i, --interval`: ポーリング間隔（秒）
  - `-d, --duration`: 監視継続時間（秒）
  - `-l, --label`: セッションラベル

```go
func NewWatchCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "watch [user_id]",
        Short: "指定した ユーザー ID の配信を定期監視します",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            userID := args[0]

            // context から設定を取得（グローバル変数不使用）
            cfg := configFromContext(cmd.Context())
            if !cfg.IsConfigured() {
                return fmt.Errorf("認証情報が設定されていません。'config set'を実行してください。")
            }

            dbClient, err := db.New(cfg.DBPath)
            if err != nil {
                return err
            }
            defer dbClient.Close()

            resolvedInterval := viper.GetInt("default_interval")
            resolvedDuration := viper.GetInt("default_duration")

            label, _ := cmd.Flags().GetString("label")
            wait, _ := cmd.Flags().GetBool("wait")
            waitInterval, _ := cmd.Flags().GetInt("wait-interval")
            timeout, _ := cmd.Flags().GetInt("timeout")

            apiClient := api.NewClient(cfg.ClientID, cfg.ClientSecret, 15*time.Second)

            opts := monitor.MonitorOptions{
                Interval:     time.Duration(resolvedInterval) * time.Second,
                Duration:     time.Duration(resolvedDuration) * time.Second,
                Label:        label,
                WaitOnConfig: wait,
                WaitInterval: time.Duration(waitInterval) * time.Second,
                WaitTimeout:  time.Duration(timeout) * time.Second,
            }

            _, err = monitor.MonitorMovie(cmd.Context(), apiClient, dbClient, userID, opts)
            return err
        },
    }

    cmd.Flags().IntP("interval", "i", 0, "ポーリング間隔 (秒)")
    cmd.Flags().IntP("duration", "d", 0, "監視時間 (秒)。省略で無限継続")
    cmd.Flags().StringP("label", "l", "", "このセッションに付けるラベル")
    cmd.Flags().BoolP("wait", "w", false, "オフライン時に配信開始を待機する")
    cmd.Flags().Int("wait-interval", 10, "待機時の配信状態チェック間隔 (秒)")
    cmd.Flags().IntP("timeout", "t", 0, "最大待機時間 (秒)。0 = 無限")

    viper.BindPFlag("default_interval", cmd.Flags().Lookup("interval"))
    viper.BindPFlag("default_duration", cmd.Flags().Lookup("duration"))

    return cmd
}
```

### 4.3. `status` コマンド

- **引数**: `user_id` (必須)
- **役割**: 指定されたユーザーの現在の配信状態（配信中であれば Movie ID やタイトル、同時視聴者数、コメント数など）を取得して表示する。
- **実装方針**: 
  - `apiClient.GetCurrentLive(ctx, userID)` を呼び出す。
  - レスポンスが `nil` の場合は「オフラインです」と表示。
  - レスポンスがある場合は、タイトル、字幕、配信者名、同時視聴者数、コメント数などをフォーマットした Rich なパネル（lipgloss を使用）でコンソールに出力する。

### 4.4. `sessions` コマンド

- **フラグ**:
  - `-m, --movie-id`: 指定 Movie ID に絞り込み
  - `-n, --limit`: 表示する最大行数 (デフォルト: 20)

### 4.5. `summary` コマンド

- **引数**: `movie_id` (必須)
- 統計サマリーテーブルをフォーマットして出力する。

### 4.6. `dashboard` コマンド

- **フラグ**:
  - `-h, --host`: ホストアドレス (デフォルト: `127.0.0.1`)
  - `-p, --port`: ポート番号 (デフォルト: `8000`)

### 4.7. `config` サブコマンド群

- **`config set`**:
  - フラグ: `--client-id`, `--client-secret`, `--db-path`
  - フラグ: `--interval`: デフォルトのポーリング間隔（秒）（デフォルト: 10）
- **`config show`**:
  - フラグ: `--verify`: 指定された場合、ロードした認証情報を使い、実際に `VerifyCredentials` API を叩いて疏通確認まで実行する。
  - 現在の設定を出力。`client_secret` はマスキング（例: `abcd****wxyz`）する。
