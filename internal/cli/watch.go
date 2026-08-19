package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"tc-analyzer/internal/api"
	"tc-analyzer/internal/db"
	"tc-analyzer/internal/monitor"
)

// NewWatchCmd returns the watch subcommand.
func NewWatchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch [user_id]",
		Short: "指定したユーザー ID の配信を定期監視します",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			userID := args[0]

			cfg := configFromContext(cmd.Context())
			if !cfg.IsConfigured() {
				return fmt.Errorf("認証情報が設定されていません。'config set' を実行してください")
			}

			dbClient, err := db.New(cfg.DBPath)
			if err != nil {
				return fmt.Errorf("DBの初期化に失敗しました: %w", err)
			}
			defer dbClient.Close()

			intervalFlag, _ := cmd.Flags().GetInt("interval")
			durationFlag, _ := cmd.Flags().GetInt("duration")
			label, _ := cmd.Flags().GetString("label")
			wait, _ := cmd.Flags().GetBool("wait")
			waitInterval, _ := cmd.Flags().GetInt("wait-interval")
			timeout, _ := cmd.Flags().GetInt("timeout")

			resolvedInterval := cfg.DefaultInterval
			if intervalFlag > 0 {
				resolvedInterval = intervalFlag
			}
			resolvedDuration := cfg.DefaultDuration
			if durationFlag > 0 {
				resolvedDuration = durationFlag
			}

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

	return cmd
}
