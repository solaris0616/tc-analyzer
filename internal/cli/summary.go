package cli

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"tc-analyzer/internal/db"
)

// NewSummaryCmd returns the summary subcommand.
func NewSummaryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "summary [movie_id]",
		Short: "指定 Movie ID のセッション統計サマリーを表示します",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			movieID := args[0]

			cfg := configFromContext(cmd.Context())

			dbClient, err := db.New(cfg.DBPath)
			if err != nil {
				return err
			}
			defer dbClient.Close()

			summary, err := dbClient.GetMovieSummary(movieID)
			if err != nil {
				return err
			}

			if summary == nil || summary.TotalRecords == 0 {
				fmt.Printf("Movie ID: %s のデータが見つかりませんでした。\n", movieID)
				return nil
			}

			boxStyle := lipgloss.NewStyle().
				Border(lipgloss.NormalBorder()).
				BorderForeground(lipgloss.Color("39")).
				Padding(1, 2)

			titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))

			content := fmt.Sprintf("%s\n\n"+
				"総記録数          : %d 回\n"+
				"最高同時視聴者数  : %d 人 (セッション記録最大: %d 人)\n"+
				"最小同時視聴者数  : %d 人\n"+
				"平均同時視聴者数  : %.1f 人\n"+
				"累計視聴者数(最大): %d 人\n"+
				"観察コメント増分  : %d 件 (最終記録コメント: %d 件)\n"+
				"初回記録日時 (UTC): %s\n"+
				"最終記録日時 (UTC): %s",
				titleStyle.Render(fmt.Sprintf("📊 配信サマリー [Movie ID: %s]", movieID)),
				summary.TotalRecords,
				summary.PeakViewers, summary.SessionMaxView,
				summary.MinViewers,
				summary.AvgViewers,
				summary.SessionTotalView,
				summary.TotalCommentsObserved, summary.FinalCommentCount,
				summary.FirstRecord.Format("2006-01-02 15:04:05"),
				summary.LastRecord.Format("2006-01-02 15:04:05"),
			)

			fmt.Println(boxStyle.Render(content))
			return nil
		},
	}

	return cmd
}
