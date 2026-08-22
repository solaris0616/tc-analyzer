package cli

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"tc-analyzer/internal/api"
	"tc-analyzer/internal/db"
)

// NewSessionsCmd returns the sessions subcommand.
func NewSessionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "過去の監視セッション一覧を表示します",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := configFromContext(cmd.Context())

			dbClient, err := db.New(cfg.DBPath)
			if err != nil {
				return err
			}
			defer dbClient.Close()

			movieID, _ := cmd.Flags().GetString("movie-id")
			limit, _ := cmd.Flags().GetInt("limit")

			sessions, err := dbClient.ListSessions(movieID)
			if err != nil {
				return err
			}

			if len(sessions) == 0 {
				fmt.Println("セッションが見つかりませんでした。")
				return nil
			}

			if limit > 0 && len(sessions) > limit {
				sessions = sessions[:limit]
			}

			headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
			fmt.Printf("%-6s | %-12s | %-20s | %s\n",
				headerStyle.Render("ID"),
				headerStyle.Render("Movie ID"),
				headerStyle.Render("開始日時 (UTC)"),
				headerStyle.Render("ラベル"),
			)
			fmt.Println("--------------------------------------------------------------------------------")

			for _, s := range sessions {
				label := s.Label
				if label == "" {
					label = "-"
				}
				startedStr := s.StartedAt.Format("2006-01-02 15:04:05")
				fmt.Printf("%-6d | %-12s | %-20s | %s\n",
					s.ID, s.MovieID, startedStr, label)
			}

			return nil
		},
	}

	cmd.Flags().StringP("movie-id", "m", "", "特定 Movie ID に絞り込み")
	cmd.Flags().IntP("limit", "n", 20, "表示する最大行数")
	cmd.AddCommand(newBackfillBroadcastersCmd())

	return cmd
}

func newBackfillBroadcastersCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "backfill-broadcasters",
		Short: "既存セッションへ配信者情報を補完します",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := configFromContext(cmd.Context())
			if !cfg.IsConfigured() {
				return fmt.Errorf("client_id または client_secret が設定されていません")
			}

			database, err := db.New(cfg.DBPath)
			if err != nil {
				return fmt.Errorf("DBの初期化に失敗しました: %w", err)
			}
			defer database.Close()

			movieIDs, err := database.ListUnattributedMovieIDs()
			if err != nil {
				return fmt.Errorf("未分類データの取得に失敗しました: %w", err)
			}
			if len(movieIDs) == 0 {
				fmt.Println("補完対象のセッションはありません。")
				return nil
			}

			client := api.NewClient(cfg.ClientID, cfg.ClientSecret, 15*time.Second)
			succeeded, failed := 0, 0
			var updated int64
			for _, movieID := range movieIDs {
				snapshot, err := client.GetMovieInfo(cmd.Context(), movieID)
				if err != nil {
					failed++
					fmt.Printf("✗ %s: %v\n", movieID, err)
					continue
				}
				count, err := database.BackfillBroadcasterForMovie(movieID, db.Broadcaster{
					ID: snapshot.BroadcasterID, ScreenID: snapshot.BroadcasterScreenID, Name: snapshot.BroadcasterName,
				})
				if err != nil {
					failed++
					fmt.Printf("✗ %s: %v\n", movieID, err)
					continue
				}
				succeeded++
				updated += count
				fmt.Printf("✓ %s: %s (@%s), %dセッション\n", movieID, snapshot.BroadcasterName, snapshot.BroadcasterScreenID, count)
			}

			fmt.Printf("完了: 成功 %d配信 / 失敗 %d配信 / 更新 %dセッション\n", succeeded, failed, updated)
			return nil
		},
	}
}
