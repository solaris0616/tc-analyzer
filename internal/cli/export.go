package cli

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"tc-analyzer/internal/db"
)

// NewExportCmd returns the export subcommand.
func NewExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export [movie_id]",
		Short: "収集したスナップショットデータを CSV や JSON 形式でエクスポートします",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			movieID := args[0]
			outPath, _ := cmd.Flags().GetString("output")
			format, _ := cmd.Flags().GetString("format")

			if outPath == "" {
				ext := ".csv"
				if format == "json" {
					ext = ".json"
				}
				outPath = fmt.Sprintf("movie_%s%s", movieID, ext)
			}

			cfg := configFromContext(cmd.Context())
			dbClient, err := db.New(cfg.DBPath)
			if err != nil {
				return err
			}
			defer dbClient.Close()

			snapshots, err := dbClient.GetMovieSnapshots(movieID)
			if err != nil {
				return err
			}
			if len(snapshots) == 0 {
				return fmt.Errorf("Movie ID: %s のデータが存在しません", movieID)
			}

			if format == "json" {
				return exportJSON(outPath, snapshots)
			}
			return exportCSV(outPath, snapshots)
		},
	}

	cmd.Flags().StringP("output", "o", "", "出力ファイルパス")
	cmd.Flags().StringP("format", "f", "csv", "フォーマット (csv, json)")

	return cmd
}

func exportCSV(path string, snapshots []*db.Snapshot) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	// Header
	header := []string{
		"id", "session_id", "recorded_at", "elapsed_sec", "is_live",
		"current_view_count", "max_view_count", "total_view_count",
		"comment_count", "comment_delta", "duration",
	}
	if err := w.Write(header); err != nil {
		return err
	}

	for _, s := range snapshots {
		isLiveStr := "0"
		if s.IsLive {
			isLiveStr = "1"
		}
		row := []string{
			strconv.FormatInt(s.ID, 10),
			strconv.FormatInt(s.SessionID, 10),
			s.RecordedAt.Format(time.RFC3339),
			strconv.Itoa(s.ElapsedSec),
			isLiveStr,
			strconv.Itoa(s.CurrentViewCount),
			strconv.Itoa(s.MaxViewCount),
			strconv.Itoa(s.TotalViewCount),
			strconv.Itoa(s.CommentCount),
			strconv.Itoa(s.CommentDelta),
			strconv.Itoa(s.Duration),
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}

	fmt.Printf("CSV エクスポート完了: %s (%d 行)\n", path, len(snapshots))
	return nil
}

func exportJSON(path string, snapshots []*db.Snapshot) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(snapshots); err != nil {
		return err
	}

	fmt.Printf("JSON エクスポート完了: %s (%d 件)\n", path, len(snapshots))
	return nil
}
