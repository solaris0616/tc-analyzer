package cli

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

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
			fmt.Printf("%-6s | %-12s | %-20s | %-8s | %s\n",
				headerStyle.Render("ID"),
				headerStyle.Render("Movie ID"),
				headerStyle.Render("開始日時 (UTC)"),
				headerStyle.Render("間隔"),
				headerStyle.Render("ラベル"),
			)
			fmt.Println("--------------------------------------------------------------------------------")

			for _, s := range sessions {
				label := s.Label
				if label == "" {
					label = "-"
				}
				startedStr := s.StartedAt.Format("2006-01-02 15:04:05")
				fmt.Printf("%-6d | %-12s | %-20s | %-6ds | %s\n",
					s.ID, s.MovieID, startedStr, s.IntervalSec, label)
			}

			return nil
		},
	}

	cmd.Flags().StringP("movie-id", "m", "", "特定 Movie ID に絞り込み")
	cmd.Flags().IntP("limit", "n", 20, "表示する最大行数")

	return cmd
}
