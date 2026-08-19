package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"tc-analyzer/internal/dashboard"
	"tc-analyzer/internal/db"
)

// NewDashboardCmd returns the dashboard subcommand.
func NewDashboardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "データ可視化 Web ダッシュボードを起動します",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := configFromContext(cmd.Context())

			dbClient, err := db.New(cfg.DBPath)
			if err != nil {
				return fmt.Errorf("DBの初期化に失敗しました: %w", err)
			}
			defer dbClient.Close()

			host, _ := cmd.Flags().GetString("host")
			port, _ := cmd.Flags().GetInt("port")

			srv := dashboard.NewServer(dbClient, host, port)
			return srv.Start(cmd.Context())
		},
	}

	cmd.Flags().StringP("host", "H", "127.0.0.1", "バインドするホストアドレス")
	cmd.Flags().IntP("port", "p", 8000, "ポート番号")

	return cmd
}
