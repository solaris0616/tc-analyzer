# 設計書 04 — 監視ループ・配信待機ロジック (`internal/monitor`)

## 1. 概要

Python 版 `monitor.py` に相当する、配信データの定期ポーリング（監視）および配信開始の待機を行うコアロジック。
Go 版では、`goroutine` と `context.Context`、`time` パッケージを活用して堅牢かつキャンセル可能なループを構築する。

---

## 2. 主要機能と設計方針

Go 版の監視・待機処理では、以下の設計方針を適用する。

1. **Context による制御**: `Ctrl+C` やタイムアウトによる監視停止要求を伝播させるため、すべての主要関数は `context.Context` を第一引数に受け取る。
2. **非同期ポーリングとタイマー調整**: ポーリング処理の実行時間を差し引いて次のポーリング開始時刻を正確に制御する（`time.Ticker` または `time.After` を使用）。
3. **安全なシャットダウン**: 割り込みシグナルを検知した際、DB 書き込み中であればそれを完了させてから安全にループを抜ける。
4. **コンソール表示**: Rich ライブラリのようなリッチなテーブル表示や更新を Go で行うため、ANSI エスケープコードを用いた画面書き換え、またはシンプルな標準出力フォーマットを提供する。

---

## 3. 関数定義

```go
package monitor

import (
    "context"
    "time"
    
    "tc-analyzer/internal/api"
    "tc-analyzer/internal/db"
)

// MonitorOptions は監視処理のオプションを保持する
type MonitorOptions struct {
    Interval     time.Duration // ポーリング間隔
    Duration     time.Duration // 監視継続時間（0 の場合は無限）
    Label        string        // セッションラベル
    WaitOnConfig bool          // オフライン時に配信開始を待機するかどうか (-w/--wait)
    WaitInterval time.Duration // 待機時の配信状態チェック間隔
    WaitTimeout  time.Duration // 最大待機時間 (0 = 無限)
    OnSnapshot   func(snap *api.MovieSnapshot, sessionID int64) // スナップショット取得時のコールバック（テスト用）
}

// MonitorMovie は指定されたユーザーの配信を監視する。
// 配信がオフラインで WaitOnConfig が有効な場合は配信開始を待機する。
// 配信中になった後は Movie ID を取得して定期監視を開始する。
func MonitorMovie(ctx context.Context, client *api.Client, database *db.DB, userID string, opts MonitorOptions) (int64, error)

// waitForLive は指定ユーザーが配信開始するまで待機する内部ヘルパー
func waitForLive(ctx context.Context, client *api.Client, userID string, pollInterval, timeout time.Duration) (string, error)
```

---

## 4. 詳細設計: `MonitorMovie`

### 4.1. ライフサイクルとループ制御

```go
func MonitorMovie(ctx context.Context, client *api.Client, database *db.DB, userID string, opts MonitorOptions) (int64, error) {
    // 0. ユーザーの存在確認（数値IDを含め、存在しないユーザーへの無限待機を防ぐ）
    if _, err := client.GetUser(ctx, userID); err != nil {
        var apiErr *api.APIError
        if errors.As(err, &apiErr) && apiErr.HTTPStatus == 404 {
            return 0, fmt.Errorf("ユーザー @%s は存在しません", userID)
        }
        return 0, fmt.Errorf("ユーザー情報の取得に失敗しました: %w", err)
    }

    // 1. 現在の配信状態を確認
    snapshot, err := client.GetCurrentLive(ctx, userID)
    if err != nil {
        return 0, fmt.Errorf("配信状態の取得に失敗しました: %w", err)
    }

    var movieID string
    if snapshot != nil && snapshot.IsLive {
        movieID = snapshot.MovieID
    } else {
        // オフライン時の分岐
        if !opts.WaitOnConfig {
            return 0, fmt.Errorf("ユーザー @%s は現在オフラインです。監視を開始するには -w/--wait フラグを指定してください。", userID)
        }
        // 配信開始を待機
        id, err := waitForLive(ctx, client, userID, opts.WaitInterval, opts.WaitTimeout)
        if err != nil {
            return 0, err
        }
        movieID = id
    }

    // 2. セッションの作成 (Movie ID が決定した段階で行う)
    intervalSec := int(opts.Interval.Seconds())
    sessionID, err := database.CreateSession(movieID, intervalSec, opts.Label)
    if err != nil {
        return 0, err
    }

    startTime := time.Now()
    pollCount := 0
    var prevCommentCount *int
    maxViewersSeen := 0

    // Duration によるタイムリミット設定
    var cancel context.CancelFunc
    if opts.Duration > 0 {
        ctx, cancel = context.WithTimeout(ctx, opts.Duration)
        defer cancel()
    }

    // Ticker による定期処理の制御
    ticker := time.NewTicker(opts.Interval)
    defer ticker.Stop()

    // 監視ライターの開始
    writer := uilive.New()
    writer.Start()
    defer writer.Stop()

    runPoll := func() bool {
        pollCount++

        // APIから配信情報を取得
        snapshot, err := client.GetMovieInfo(ctx, movieID)
        if err != nil {
            log.Printf("⚠ API エラー (poll #%d): %v\n", pollCount, err)
            return true
        }

        // コメント増分の算出
        commentDelta := 0
        if prevCommentCount != nil {
            delta := snapshot.CommentCount - *prevCommentCount
            if delta > 0 {
                commentDelta = delta
            }
        }
        currentComments := snapshot.CommentCount
        prevCommentCount = &currentComments

        // 最大同時視聴数の更新
        if snapshot.CurrentViewCount > maxViewersSeen {
            maxViewersSeen = snapshot.CurrentViewCount
        }

        elapsedSec := int(time.Since(startTime).Seconds())

        // DBへの保存
        _, err = database.AddSnapshot(sessionID, snapshot, elapsedSec, commentDelta)
        if err != nil {
            log.Printf("⚠ DB 保存エラー: %v\n", err)
        }

        if opts.OnSnapshot != nil {
            opts.OnSnapshot(snapshot, sessionID)
        }

        // TUI画面の更新
        updateLivePanel(writer, snapshot, elapsedSec, pollCount, commentDelta, int(opts.Interval.Seconds()), maxViewersSeen, sessionID)

        // 配信がオフラインになったら終了
        if !snapshot.IsLive {
            fmt.Println("\n[yellow]⚠ 配信がオフラインになりました。定期監視を自動終了します。[/yellow]")
            return false
        }

        return true
    }

    // 初回ポーリング
    if !runPoll() {
        return sessionID, nil
    }

    for {
        select {
        case <-ctx.Done():
            if ctx.Err() == context.DeadlineExceeded && opts.Duration > 0 {
                fmt.Println("\n[cyan]監視時間（duration）に達したため、監視を終了します。[/cyan]")
            }
            return sessionID, nil
        case <-ticker.C:
            if !runPoll() {
                return sessionID, nil
            }
        }
    }
}
```

---

## 5. 詳細設計: `waitForLive`

### 5.1. 配信待機ループ

```go
func waitForLive(ctx context.Context, client *api.Client, userID string, pollInterval, timeout time.Duration) (string, error) {
    startTime := time.Now()
    checkCount := 0

    // Timeout による制御
    if timeout > 0 {
        var cancel context.CancelFunc
        ctx, cancel = context.WithTimeout(ctx, timeout)
        defer cancel()
    }

    ticker := time.NewTicker(pollInterval)
    defer ticker.Stop()

    runCheck := func() (string, bool, error) {
        checkCount++
        
        // ユーザーの現在のライブ情報を取得
        snapshot, err := client.GetCurrentLive(ctx, userID)
        if err != nil {
            log.Printf("⚠ API エラー (確認 #%d): %v\n", checkCount, err)
            return "", false, nil // リトライするためループは継続
        }

        if snapshot != nil && snapshot.IsLive {
            return snapshot.MovieID, true, nil // 配信開始検出
        }

        // コンソール表示の更新 (待機中ステータス)
        elapsedSec := int(time.Since(startTime).Seconds())
        updateWaitPanel(userID, checkCount, elapsedSec, timeout, pollInterval)

        return "", false, nil
    }

    // 初回チェック
    if movieID, found, err := runCheck(); found || err != nil {
        return movieID, err
    }

    for {
        select {
        case <-ctx.Done():
            if ctx.Err() == context.DeadlineExceeded {
                return "", fmt.Errorf("timeout waiting for user @%s to go live", userID)
            }
            return "", ctx.Err()
        case <-ticker.C:
            if movieID, found, err := runCheck(); found || err != nil {
                return movieID, err
            }
        }
    }
}
```

---

## 6. コンソール表示 (TUI) の実装アプローチ

Python 版では `rich.live.Live` を使用して、ターミナル上で表をインプレースで更新していた。Go 版では、同様のちらつきのないライブ更新とリサイズ破綻防止のために **`github.com/gosuri/uilive`** を採用する。

### uilive を用いた更新設計

`uilive.Writer` を監視ループ開始時に初期化し、ループ内の表示更新処理でそのライターに対して描画内容を出力する。

```go
import "github.com/gosuri/uilive"

func MonitorMovie(...) {
    // uilive ライターの開始
    writer := uilive.New()
    writer.Start()
    defer writer.Stop()

    runPoll := func() bool {
        ...
        // writer.Bypass を使えば通常のログ（エラー等）を出力に混ぜても表示が壊れない
        // 描画バッファへの書き込み
        updateLivePanel(writer, snapshot, elapsedSec, pollCount, commentDelta, interval, maxViewersSeen, sessionID)
        ...
    }
}
```

### 表示内容の Go 実装例（uilive + ANSI カラー）

```go
func updateLivePanel(w io.Writer, snap *api.MovieSnapshot, elapsedSec, pollCount, commentDelta, interval, maxSeen int, sessionID int64) {
    status := "\033[31;1m● LIVE\033[0m"
    if !snap.IsLive {
        status = "\033[90m○ OFFLINE / RECORDED\033[0m"
    }

    // uilive.Writer に対して出力することで、インプレースに書き換えられる
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
}
```

---

## 7. Python 版との比較・主要な設計差分

| 調査項目 | Python 版 | Go 版 |
|---|---|---|
| 非同期/並行処理 | `anyio` / `asyncio` による `sleep` | `select` + `time.Ticker` と `context.Context` |
| キャンセル処理 | `KeyboardInterrupt` による例外処理 | `context.Context` シグナル通知による安全な終了 |
| 終了条件判定 | ポーリングループ内の `is_live` チェック | チャネル経由での状態監視、および context タイムアウト判定 |
