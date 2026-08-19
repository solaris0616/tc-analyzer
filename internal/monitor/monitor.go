package monitor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/gosuri/uilive"

	"tc-analyzer/internal/api"
	"tc-analyzer/internal/db"
)

// MonitorOptions defines parameters for monitoring a user's stream.
type MonitorOptions struct {
	Interval     time.Duration
	Duration     time.Duration
	Label        string
	WaitOnConfig bool
	WaitInterval time.Duration
	WaitTimeout  time.Duration
	Writer       io.Writer // Custom output writer (optional, defaults to uilive)
	OnSnapshot   func(snap *api.MovieSnapshot, sessionID int64)
	// CommentInterval is the polling interval for comment collection (default: 15s).
	CommentInterval time.Duration
}

// MonitorMovie monitors a user's stream, polling data periodically.
func MonitorMovie(ctx context.Context, client *api.Client, database *db.DB, userID string, opts MonitorOptions) (int64, error) {
	// 0. Verify user exists
	user, err := client.GetUser(ctx, userID)
	if err != nil {
		var apiErr *api.APIError
		if errors.As(err, &apiErr) && apiErr.HTTPStatus == 404 {
			return 0, fmt.Errorf("ユーザー @%s は存在しません", userID)
		}
		return 0, fmt.Errorf("ユーザー情報の取得に失敗しました: %w", err)
	}

	// Default intervals if not set
	if opts.Interval <= 0 {
		opts.Interval = 10 * time.Second
	}
	if opts.WaitInterval <= 0 {
		opts.WaitInterval = 10 * time.Second
	}

	// 1. Check current live status
	snapshot, err := client.GetCurrentLive(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("配信状態の取得に失敗しました: %w", err)
	}

	var movieID string
	if snapshot != nil && snapshot.IsLive {
		movieID = snapshot.MovieID
	} else {
		if !opts.WaitOnConfig {
			return 0, fmt.Errorf("ユーザー @%s は現在オフラインです。監視を開始するには -w/--wait フラグを指定してください", userID)
		}
		// Wait for live
		liveSnapshot, err := waitForLive(ctx, client, userID, opts.WaitInterval, opts.WaitTimeout, opts.Writer)
		if err != nil {
			return 0, err
		}
		snapshot = liveSnapshot
		movieID = snapshot.MovieID
	}

	// 2. Create database session
	intervalSec := int(opts.Interval.Seconds())
	broadcaster := db.Broadcaster{
		ID:       snapshot.BroadcasterID,
		ScreenID: snapshot.BroadcasterScreenID,
		Name:     snapshot.BroadcasterName,
	}
	if broadcaster.ID == "" {
		broadcaster.ID = user.ID
	}
	if broadcaster.ScreenID == "" {
		broadcaster.ScreenID = user.ScreenID
	}
	if broadcaster.Name == "" {
		broadcaster.Name = user.Name
	}
	if broadcaster.ID == "" {
		return 0, fmt.Errorf("配信者IDを取得できませんでした")
	}
	sessionID, err := database.CreateSessionWithBroadcaster(movieID, intervalSec, opts.Label, broadcaster)
	if err != nil {
		return 0, fmt.Errorf("セッションの作成に失敗しました: %w", err)
	}

	startTime := time.Now()
	pollCount := 0
	var prevCommentCount *int
	maxViewersSeen := 0

	if opts.Duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Duration)
		defer cancel()
	}

	// --- Comment polling goroutine ---
	commentInterval := opts.CommentInterval
	if commentInterval <= 0 {
		commentInterval = 15 * time.Second
	}
	commentCtx, cancelComments := context.WithCancel(ctx)
	var commentWG sync.WaitGroup
	commentWG.Add(1)
	go func() {
		defer commentWG.Done()
		ticker := time.NewTicker(commentInterval)
		defer ticker.Stop()
		var sliceID string
		for {
			select {
			case <-commentCtx.Done():
				return
			case <-ticker.C:
				resp, err := client.GetComments(commentCtx, movieID, sliceID, 50)
				if err != nil {
					if commentCtx.Err() != nil {
						return
					}
					log.Printf("⚠ コメント取得エラー: %v\n", err)
					continue
				}
				for _, c := range resp.Comments {
					if _, err := database.RecordComment(movieID, c.ID, c.FromUser.ID, c.FromUser.ScreenID, c.FromUser.Name, c.Message, c.Created); err != nil {
						log.Printf("⚠ コメント保存エラー: %v\n", err)
					}
					// Track newest comment ID for next incremental fetch
					if sliceID == "" || c.ID > sliceID {
						sliceID = c.ID
					}
				}
			}
		}
	}()
	defer func() {
		cancelComments()
		commentWG.Wait()
	}()

	ticker := time.NewTicker(opts.Interval)
	defer ticker.Stop()

	var liveWriter *uilive.Writer
	outWriter := opts.Writer
	if outWriter == nil {
		liveWriter = uilive.New()
		liveWriter.Start()
		defer liveWriter.Stop()
		outWriter = liveWriter
	}

	runPoll := func() bool {
		pollCount++

		snap, err := client.GetMovieInfo(ctx, movieID)
		if err != nil {
			log.Printf("⚠ API エラー (poll #%d): %v\n", pollCount, err)
			return true
		}
		if snap.BroadcasterID != "" && snap.BroadcasterID != broadcaster.ID {
			log.Printf("⚠ 配信者IDが変化したためスナップショットを保存しません (expected=%s, actual=%s)\n", broadcaster.ID, snap.BroadcasterID)
			return true
		}

		commentDelta := 0
		if prevCommentCount != nil {
			delta := snap.CommentCount - *prevCommentCount
			if delta > 0 {
				commentDelta = delta
			}
		}
		currComments := snap.CommentCount
		prevCommentCount = &currComments

		if snap.CurrentViewCount > maxViewersSeen {
			maxViewersSeen = snap.CurrentViewCount
		}

		elapsedSec := int(time.Since(startTime).Seconds())

		if _, err := database.AddSnapshot(sessionID, snap, elapsedSec, commentDelta); err != nil {
			log.Printf("⚠ DB 保存エラー: %v\n", err)
		}

		if snap.Title != "" {
			_ = database.UpdateSessionTitle(sessionID, snap.Title)
		}

		if opts.OnSnapshot != nil {
			opts.OnSnapshot(snap, sessionID)
		}

		updateLivePanel(outWriter, snap, elapsedSec, pollCount, commentDelta, intervalSec, maxViewersSeen, sessionID)

		if !snap.IsLive {
			fmt.Fprintln(outWriter, "\n⚠ 配信がオフラインになりました。定期監視を自動終了します。")
			return false
		}

		return true
	}

	if !runPoll() {
		return sessionID, nil
	}

	for {
		select {
		case <-ctx.Done():
			if ctx.Err() == context.DeadlineExceeded && opts.Duration > 0 {
				fmt.Fprintln(outWriter, "\n監視時間 (duration) に達したため、監視を終了します。")
			}
			return sessionID, nil
		case <-ticker.C:
			if !runPoll() {
				return sessionID, nil
			}
		}
	}
}

func waitForLive(ctx context.Context, client *api.Client, userID string, pollInterval, timeout time.Duration, out io.Writer) (*api.MovieSnapshot, error) {
	startTime := time.Now()
	checkCount := 0

	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	var liveWriter *uilive.Writer
	outWriter := out
	if outWriter == nil {
		liveWriter = uilive.New()
		liveWriter.Start()
		defer liveWriter.Stop()
		outWriter = liveWriter
	}

	runCheck := func() (*api.MovieSnapshot, bool, error) {
		checkCount++

		snapshot, err := client.GetCurrentLive(ctx, userID)
		if err != nil {
			log.Printf("⚠ API エラー (確認 #%d): %v\n", checkCount, err)
			return nil, false, nil
		}

		if snapshot != nil && snapshot.IsLive {
			return snapshot, true, nil
		}

		elapsedSec := int(time.Since(startTime).Seconds())
		updateWaitPanel(outWriter, userID, checkCount, elapsedSec, timeout, pollInterval)

		return nil, false, nil
	}

	if snapshot, found, err := runCheck(); found || err != nil {
		return snapshot, err
	}

	for {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil, fmt.Errorf("ユーザー @%s の配信開始待機がタイムアウトしました", userID)
			}
			return nil, ctx.Err()
		case <-ticker.C:
			if snapshot, found, err := runCheck(); found || err != nil {
				return snapshot, err
			}
		}
	}
}

func updateLivePanel(w io.Writer, snap *api.MovieSnapshot, elapsedSec, pollCount, commentDelta, interval, maxSeen int, sessionID int64) {
	status := "\033[31;1m● LIVE\033[0m"
	if !snap.IsLive {
		status = "\033[90m○ OFFLINE / RECORDED\033[0m"
	}

	fmt.Fprintf(w, "=============================================\n")
	fmt.Fprintf(w, " 配信者      : %s (@%s)\n", snap.BroadcasterName, snap.BroadcasterScreenID)
	fmt.Fprintf(w, " タイトル    : %s\n", snap.Title)
	fmt.Fprintf(w, " Movie ID   : %s\n", snap.MovieID)
	fmt.Fprintf(w, " ステータス  : %s\n", status)
	fmt.Fprintf(w, "---------------------------------------------\n")
	fmt.Fprintf(w, " 同時視聴者数: \033[33;1m%d\033[0m 人\n", snap.CurrentViewCount)
	fmt.Fprintf(w, " 最大同時視聴: \033[32m%d\033[0m 人 (セッション最大: \033[92m%d\033[0m 人)\n", snap.MaxViewCount, maxSeen)
	fmt.Fprintf(w, " コメント総数: \033[35m%d\033[0m 件 (増分: \033[35;1m+%d\033[0m)\n", snap.CommentCount, commentDelta)
	fmt.Fprintf(w, " 監視経過時間: %s\n", formatDuration(time.Duration(elapsedSec)*time.Second))
	fmt.Fprintf(w, " ポーリング  : %d 回 (間隔: %d 秒)\n", pollCount, interval)
	fmt.Fprintf(w, " Session ID  : %d\n", sessionID)
	fmt.Fprintf(w, "=============================================\n")
	if lw, ok := w.(*uilive.Writer); ok {
		lw.Flush()
	}
}

func updateWaitPanel(w io.Writer, userID string, checkCount, elapsedSec int, timeout, interval time.Duration) {
	timeoutStr := "無限"
	if timeout > 0 {
		timeoutStr = formatDuration(timeout)
	}

	fmt.Fprintf(w, "=============================================\n")
	fmt.Fprintf(w, " 配信待機中  : @%s\n", userID)
	fmt.Fprintf(w, " 確認回数    : %d 回\n", checkCount)
	fmt.Fprintf(w, " 経過時間    : %s (最大待機: %s)\n", formatDuration(time.Duration(elapsedSec)*time.Second), timeoutStr)
	fmt.Fprintf(w, " チェック間隔: %d 秒\n", int(interval.Seconds()))
	fmt.Fprintf(w, "=============================================\n")
	if lw, ok := w.(*uilive.Writer); ok {
		lw.Flush()
	}
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}
