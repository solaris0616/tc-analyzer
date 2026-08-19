package cli

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"tc-analyzer/internal/api"
)

// NewStatusCmd returns the status subcommand.
func NewStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status [user_id]",
		Short: "指定ユーザーの現在の配信ステータスを表示します",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			userID := args[0]

			cfg := configFromContext(cmd.Context())
			if !cfg.IsConfigured() {
				return fmt.Errorf("認証情報が設定されていません。'config set' を実行してください")
			}

			apiClient := api.NewClient(cfg.ClientID, cfg.ClientSecret, 0)

			// ユーザーチェック
			userInfo, err := apiClient.GetUser(cmd.Context(), userID)
			if err != nil {
				return err
			}

			live, err := apiClient.GetCurrentLive(cmd.Context(), userID)
			if err != nil {
				return err
			}

			boxStyle := lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("63")).
				Padding(1, 2).
				Width(50)

			titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
			labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
			liveStatus := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9")).Render("● LIVE")
			offlineStatus := lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Render("○ OFFLINE")

			if live == nil || !live.IsLive {
				content := fmt.Sprintf("%s (%s)\nステータス: %s",
					titleStyle.Render(userInfo.Name),
					labelStyle.Render("@"+userInfo.ScreenID),
					offlineStatus,
				)
				fmt.Println(boxStyle.Render(content))
				return nil
			}

			subtitle := ""
			if live.Subtitle != "" {
				subtitle = fmt.Sprintf("\n%s", labelStyle.Render(live.Subtitle))
			}

			content := fmt.Sprintf("%s (%s)\nタイトル: %s%s\nステータス: %s\nMovie ID: %s\n同時視聴者数: %d 人 (最大: %d 人)\nコメント数: %d 件",
				titleStyle.Render(live.BroadcasterName),
				labelStyle.Render("@"+live.BroadcasterScreenID),
				live.Title,
				subtitle,
				liveStatus,
				live.MovieID,
				live.CurrentViewCount,
				live.MaxViewCount,
				live.CommentCount,
			)

			fmt.Println(boxStyle.Render(content))
			return nil
		},
	}

	return cmd
}
