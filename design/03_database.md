# 設計書 03 — データベース (`internal/db`)

## 1. 概要

`modernc.org/sqlite`（pure Go、CGO不要）を使用するSQLiteデータアクセス層。

---

## 2. スキーマ

```sql
CREATE TABLE IF NOT EXISTS sessions (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    movie_id     TEXT    NOT NULL,
    started_at   TEXT    NOT NULL,   -- ISO8601 UTC 例: 2024-01-15T10:30:00+00:00
    label        TEXT,               -- ユーザーが付けたラベル (NULL 可)
    title        TEXT,
    broadcaster_id        TEXT,
    broadcaster_screen_id TEXT,
    broadcaster_name      TEXT
);

CREATE INDEX IF NOT EXISTS idx_sessions_broadcaster
    ON sessions(broadcaster_id, movie_id, started_at);

CREATE TABLE IF NOT EXISTS snapshots (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id          INTEGER NOT NULL REFERENCES sessions(id),
    recorded_at         TEXT    NOT NULL,   -- ISO8601 UTC
    current_view_count  INTEGER NOT NULL,
    max_view_count      INTEGER NOT NULL,
    total_view_count    INTEGER NOT NULL,
    comment_count       INTEGER NOT NULL,
    duration            INTEGER NOT NULL    -- 配信経過秒数（APIから）
);

CREATE INDEX IF NOT EXISTS idx_snapshots_session ON snapshots(session_id, recorded_at);
CREATE INDEX IF NOT EXISTS idx_snapshots_movie   ON snapshots(session_id);

CREATE TABLE IF NOT EXISTS commenters (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    movie_id      TEXT    NOT NULL,
    user_id       TEXT    NOT NULL,
    screen_id     TEXT    NOT NULL,
    name          TEXT    NOT NULL,
    comment_count INTEGER NOT NULL DEFAULT 1,
    first_seen_at TEXT    NOT NULL,
    last_seen_at  TEXT    NOT NULL,
    UNIQUE(movie_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_commenters_movie ON commenters(movie_id);

CREATE TABLE IF NOT EXISTS comment_logs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    movie_id    TEXT    NOT NULL,
    comment_id  TEXT    NOT NULL UNIQUE,
    user_id     TEXT    NOT NULL,
    screen_id   TEXT    NOT NULL,
    message     TEXT    NOT NULL,
    created_at  TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_comment_logs_movie ON comment_logs(movie_id, created_at);
```

> **重要**: 導出可能な `interval_sec`、`elapsed_sec`、`is_live`、`comment_delta` は保持しない。

---

## 3. データ型定義

```go
package db

import "time"

// Session は sessions テーブルの 1 行を表す
type Session struct {
    ID                  int64
    MovieID             string
    StartedAt           time.Time
    Label               string
    Title               string
    BroadcasterID       string
    BroadcasterScreenID string
    BroadcasterName     string
}

// Snapshot は snapshots テーブルの 1 行を表す
type Snapshot struct {
    ID               int64
    SessionID        int64
    RecordedAt       time.Time
    CurrentViewCount int
    MaxViewCount     int
    TotalViewCount   int
    CommentCount     int
    Duration         int
    CumulativeCommenters int // APIレスポンス用の算出値
}

// SessionSummary はセッション集計結果
type SessionSummary struct {
    TotalRecords          int
    MinViewers            int
    PeakViewers           int
    AvgViewers            float64
    SessionMaxView        int
    SessionTotalView      int
    FinalCommentCount     int
    TotalCommentsObserved int
    FirstRecord           time.Time
    LastRecord            time.Time
}

// MovieListRow は list_movies クエリの結果行
type MovieListRow struct {
    MovieID      string
    StartedAt    time.Time
    Label        string
    TotalRecords int
    Title        string
}

type Broadcaster struct {
    ID       string
    ScreenID string
    Name     string
}

type BroadcasterListRow struct {
    ID           string
    ScreenID     string
    Name         string
    MovieCount   int
    SessionCount int
    LastSeenAt   time.Time
}

type AnalysisRow struct {
    DayOfWeek    int
    HourOfDay    int
    MinuteOfHour int
    AvgViewers   float64
    MaxViewers   int
    DataPoints   int
}

type Commenter struct {
    ID           int64
    MovieID      string
    UserID       string
    ScreenID     string
    Name         string
    CommentCount int
    FirstSeenAt  time.Time
    LastSeenAt   time.Time
}

type CommentLog struct {
    ID        int64
    MovieID   string
    CommentID string
    UserID    string
    ScreenID  string
    Message   string
    CreatedAt time.Time
}
```

---

## 4. DB 型定義

```go
// DB は SQLite データベースラッパー
type DB struct {
    path string
    db   *sql.DB
}

// New は DB を初期化し、スキーマを作成する
func New(dbPath string) (*DB, error)

// Close は DB 接続を閉じる
func (d *DB) Close() error
```

---

## 5. 公開メソッド一覧

### セッション操作

```go
// CreateSession は新しい監視セッションを作成し、session_id を返す
func (d *DB) CreateSession(movieID string, label string) (int64, error)

// GetSession は session_id に対応するセッションを返す
func (d *DB) GetSession(sessionID int64) (*Session, error)

// ListSessions は全セッション（または movie_id で絞り込み）を返す
// movie_id が空文字の場合は全件返す
func (d *DB) ListSessions(movieID string) ([]*Session, error)

// GetLatestSessionByMovie は指定 movie_id の最新セッションを返す
func (d *DB) GetLatestSessionByMovie(movieID string) (*Session, error)

// ListMovies は movie_id ごとにグループ化した一覧を返す
func (d *DB) ListMovies() ([]*MovieListRow, error)
```

### スナップショット操作

```go
// AddSnapshot はスナップショットを保存し、行IDを返す
func (d *DB) AddSnapshot(
    sessionID int64,
    snap *api.MovieSnapshot,
) (int64, error)

// GetSnapshots はセッションのスナップショット一覧を返す（昇順）
func (d *DB) GetSnapshots(sessionID int64) ([]*Snapshot, error)

// GetLatestSnapshot はセッションの最新スナップショットを返す
func (d *DB) GetLatestSnapshot(sessionID int64) (*Snapshot, error)

// GetMovieSnapshots は movie_id に紐づく全スナップショットを返す
func (d *DB) GetMovieSnapshots(movieID string) ([]*Snapshot, error)
```

### 集計

```go
// GetSessionSummary はセッション全体の集計サマリーを返す
func (d *DB) GetSessionSummary(sessionID int64) (*SessionSummary, error)

// GetMovieSummary は movie_id 全体の集計サマリーを返す
func (d *DB) GetMovieSummary(movieID string) (*SessionSummary, error)
```

### ダッシュボード用

```go
// GetAnalysisData は曜日・時間帯別の集計データを返す（JST 換算）
// AnalysisRow は {DayOfWeek, HourOfDay, MinuteOfHour, AvgViewers, MaxViewers, DataPoints} を持つ
func (d *DB) GetAnalysisData(broadcasterID string) ([]*AnalysisRow, error)
```

配信者別ダッシュボードと配信者情報の補完については
[設計書07](07_broadcaster_dashboard.md)を参照する。

---

## 6. 実装詳細

### New() の初期化フロー

SQLiteは並行書き込みで`database is locked`エラーが発生しやすいため、以下の初期化設定を行う。
1. DSN 接続文字列に `_busy_timeout=5000` を付与し、ロック時に即座にコケず5秒間リトライ・待機させる。
2. `database/sql` の接続プール設定で `SetMaxOpenConns(1)` を指定し、書き込み衝突を Go レベルで直列化する。

```go
func New(dbPath string) (*DB, error) {
    // 1. ディレクトリ作成
    os.MkdirAll(filepath.Dir(dbPath), 0755)

    // 2. DSN の設定（タイムアウト設定を付与）
    dsn := fmt.Sprintf("%s?_busy_timeout=5000", dbPath)

    // 3. DB 接続
    sqlDB, err := sql.Open("sqlite", dsn)
    if err != nil {
        return nil, err
    }

    // 4. 同時実行制御（接続数を1つに制限）
    sqlDB.SetMaxOpenConns(1)

    // 5. PRAGMA 設定
    sqlDB.Exec("PRAGMA journal_mode=WAL")
    sqlDB.Exec("PRAGMA foreign_keys=ON")

    // 6. スキーマ初期化
    if _, err := sqlDB.Exec(schemaSQL); err != nil {
        sqlDB.Close()
        return nil, err
    }

    return &DB{path: dbPath, db: sqlDB}, nil
}
```

### ISO8601 時刻変換

以下の形式でUTC時刻を保存する：

```go
time.Now().UTC().Format(time.RFC3339)
// 例: "2024-01-15T10:30:00Z"
```

### GetMovieSummary SQL

```sql
WITH ordered AS (
    SELECT sn.*,
           LAG(sn.comment_count) OVER (
               PARTITION BY sn.session_id ORDER BY sn.recorded_at, sn.id
           ) AS previous_comment_count
    FROM snapshots sn
    JOIN sessions s ON sn.session_id = s.id
    WHERE s.movie_id = ?
)
SELECT
    COUNT(*)                        AS total_records,
    MIN(current_view_count)         AS min_viewers,
    MAX(current_view_count)         AS peak_viewers,
    AVG(current_view_count)         AS avg_viewers,
    MAX(
        (SELECT MAX(max_view_count) FROM snapshots
         WHERE session_id IN (SELECT id FROM sessions WHERE movie_id = ?)),
        MAX(current_view_count)
    )                               AS session_max_view,
    MAX(
        (SELECT MAX(total_view_count) FROM snapshots
         WHERE session_id IN (SELECT id FROM sessions WHERE movie_id = ?)),
        MAX(total_view_count)
    )                               AS session_total_view,
    MAX(comment_count)              AS final_comment_count,
    SUM(CASE
        WHEN previous_comment_count IS NOT NULL
         AND comment_count > previous_comment_count
        THEN comment_count - previous_comment_count
        ELSE 0
    END)                            AS total_comments_observed,
    MIN(recorded_at)                AS first_record,
    MAX(recorded_at)                AS last_record
FROM ordered
```
