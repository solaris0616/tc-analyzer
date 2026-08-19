package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"tc-analyzer/internal/config"
)

type configKey struct{}

var configPath string

// Execute runs the root command with signal notification context.
func Execute() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rootCmd := NewRootCmd()
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}

// NewRootCmd initializes the root command and subcommands.
func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "tc-analyzer",
		Short:         "TwitCasting 配信データ解析・収集 CLI",
		Long:          "TwitCasting.tv の同時視聴者数・コメント数を定期監視して SQLite に保存します。",
		SilenceUsage:  true,
		SilenceErrors: false,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("設定のロードに失敗しました: %w", err)
			}
			ctx := context.WithValue(cmd.Context(), configKey{}, cfg)
			cmd.SetContext(ctx)
			return nil
		},
	}

	cmd.PersistentFlags().StringVar(&configPath, "config", "", "設定ファイルのパス")

	cmd.AddCommand(NewWatchCmd())
	cmd.AddCommand(NewStatusCmd())
	cmd.AddCommand(NewSessionsCmd())
	cmd.AddCommand(NewSummaryCmd())
	cmd.AddCommand(NewExportCmd())
	cmd.AddCommand(NewDashboardCmd())
	cmd.AddCommand(NewConfigCmd())

	return cmd
}

func configFromContext(ctx context.Context) *config.Config {
	cfg, _ := ctx.Value(configKey{}).(*config.Config)
	return cfg
}
