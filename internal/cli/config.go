package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"tc-analyzer/internal/api"
	"tc-analyzer/internal/config"
)

// NewConfigCmd returns the config subcommand group.
func NewConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "認証情報やデフォルト動作の設定を管理します",
	}

	cmd.AddCommand(newConfigSetCmd())
	cmd.AddCommand(newConfigShowCmd())
	cmd.AddCommand(newConfigVerifyCmd())

	return cmd
}

func newConfigSetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set",
		Short: "設定項目を保存します",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := configFromContext(cmd.Context())

			clientID, _ := cmd.Flags().GetString("client-id")
			clientSecret, _ := cmd.Flags().GetString("client-secret")
			dbPath, _ := cmd.Flags().GetString("db-path")
			interval, _ := cmd.Flags().GetInt("interval")
			duration, _ := cmd.Flags().GetInt("duration")

			if clientID != "" {
				cfg.ClientID = clientID
			}
			if clientSecret != "" {
				cfg.ClientSecret = clientSecret
			}
			if dbPath != "" {
				cfg.DBPath = dbPath
			}
			if interval > 0 {
				cfg.DefaultInterval = interval
			}
			if duration >= 0 && cmd.Flags().Changed("duration") {
				cfg.DefaultDuration = duration
			}

			if err := config.Save(configPath, cfg); err != nil {
				return fmt.Errorf("設定の保存に失敗しました: %w", err)
			}

			fmt.Println("✓ 設定を正常に保存しました。")
			return nil
		},
	}

	cmd.Flags().String("client-id", "", "TwitCasting API Client ID")
	cmd.Flags().String("client-secret", "", "TwitCasting API Client Secret")
	cmd.Flags().String("db-path", "", "SQLite DB 保存パス")
	cmd.Flags().IntP("interval", "i", 0, "デフォルトのポーリング間隔 (秒)")
	cmd.Flags().IntP("duration", "d", 0, "デフォルトの監視時間 (秒)")

	return cmd
}

func newConfigShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show",
		Short: "現在の設定内容を表示します",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := configFromContext(cmd.Context())

			maskedSecret := maskSecret(cfg.ClientSecret)

			fmt.Printf("設定ファイル : %s\n", configPathOrDefault())
			fmt.Printf("client_id    : %s\n", cfg.ClientID)
			fmt.Printf("client_secret: %s\n", maskedSecret)
			fmt.Printf("db_path      : %s\n", cfg.DBPath)
			fmt.Printf("interval     : %d 秒\n", cfg.DefaultInterval)
			fmt.Printf("duration     : %d 秒\n", cfg.DefaultDuration)

			verify, _ := cmd.Flags().GetBool("verify")
			if verify {
				fmt.Println("\n--- API 認証接続テスト ---")
				return runVerify(cmd.Context(), cfg)
			}

			return nil
		},
	}

	cmd.Flags().Bool("verify", false, "設定確認時に API 接続テストを実行する")

	return cmd
}

func newConfigVerifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "設定された認証情報を使って API 接続テストを実行します",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := configFromContext(cmd.Context())
			return runVerify(cmd.Context(), cfg)
		},
	}

	return cmd
}

func runVerify(ctx context.Context, cfg *config.Config) error {
	if !cfg.IsConfigured() {
		return fmt.Errorf("client_id または client_secret が設定されていません")
	}

	client := api.NewClient(cfg.ClientID, cfg.ClientSecret, 0)
	res, err := client.VerifyCredentials(ctx)
	if err != nil {
		return fmt.Errorf("API 接続テスト失敗: %w", err)
	}

	fmt.Printf("✓ 認証成功!\n  App Name : %s (ClientID: %s)\n  User Name: %s (@%s)\n",
		res.App.Name, res.App.ClientID, res.User.Name, res.User.ScreenID)
	return nil
}

func maskSecret(secret string) string {
	if secret == "" {
		return "(未設定)"
	}
	if len(secret) <= 8 {
		return "********"
	}
	return secret[:4] + strings.Repeat("*", len(secret)-8) + secret[len(secret)-4:]
}

func configPathOrDefault() string {
	if configPath != "" {
		return configPath
	}
	return config.DefaultConfigFile()
}
