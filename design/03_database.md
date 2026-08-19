# 設計書 03 — データベース (`internal/db`)

## 1. 概要

Python 版 `db.py` に相当する SQLite データアクセス層。
`modernc.org/sqlite` (pure Go, CGO 不要) を使用する。

---

## 2. スキーマ（Python 版と完全互換）

```sql
CREATE TABLE IF NOT EXISTS sessions (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    movie_id     TEXT    NOT NULL,
    started_at   TEXT    NOT NULL,   -- ISO8601 UTC 例: 2024-01-15T10:30:00+00:00
    label        TEXT,               -- ユーザーが付けたラベル (NULL 可)
    interval_sec INTEGER NOT NULL DEFAULT 10
);

CREATE TABLE IF NOT EXISTS snapshots (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id          INTEGER NOT NULL REFERENCES sessions(id),
    recorded_at         TEXT    NOT NULL,   -- ISO8601 UTC
    elapsed_sec         INTEGER NOT NULL,   -- セッション開始からの経過秒数
    is_live             INTEGER NOT NULL,   -- 0 or 1
    current_view_count  INTEGER NOT NULL,
    max_view_count      INTEGER NOT NULL,
    total_view_count    INTEGER NOT NULL,
    comment_count       INTEGER NOT NULL,
    comment_delta       INTEGER NOT NULL,   -- 前回からのコメント増分
    duration            INTEGER NOT NULL    -- 配信経過秒数（APIから）
);

CREATE INDEX IF NOT EXISTS idx_snapshots_session ON snapshots(session_id, recorded_at);
CREATE INDEX IF NOT EXISTS idx_snapshots_movie   ON snapshots(session_id);
```

> **重要**: このスキーマは Python 版と完全に同一にする。
> Python 版で収集した DB をそのまま Go 版で参照できる。

---

## 3. データ型定義

```go
package db

import "time"

// Session は sessions テーブルの 1 行を表す
type Session struct {
    ID          int64
    MovieID     string
    StartedAt   time.Time
    Label       string    // NULL の場合は空文字
    IntervalSec int
}

// Snapshot は snapshots テーブルの 1 行を表す
type Snapshot struct {
    ID               int64
    SessionID        int64
    RecordedAt       time.Time
    ElapsedSec       int
    IsLive           bool
    CurrentViewCount int
    MaxViewCount     int
    TotalViewCount   int
    CommentCount     int
    CommentDelta     int
    Duration         int
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
    IntervalSec  int
    TotalRecords int
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
func (d *DB) CreateSession(movieID string, intervalSec int, label string) (int64, error)

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
    elapsedSec int,
    commentDelta int,
) (int64, error)

// GetSnapshots はセッションのスナップショット一覧を返す（昇順）
func (d *DB) GetSnapshots(sessionID int64) ([]*Snapshot, error)

// GetLatestSnapshot はセッションの最新スナップショットを返す
func (d *DB) GetLatestSnapshot(sessionID int64) (*Snapshot, error)

// GetMovieSnapshots は movie_id に紐づく全スナップショット（is_live=1 のみ）を返す
func (d *DB) GetMovieSnapshots(movieID string) ([]*Snapshot, error)
```

### 集計

```go
// GetSessionSummary はセッション全体の集計サマリーを返す
func (d *DB) GetSessionSummary(sessionID int64) (*SessionSummary, error)

// GetMovieSummary は movie_id 全体の集計サマリー（is_live=1 のみ）を返す
func (d *DB) GetMovieSummary(movieID string) (*SessionSummary, error)
```

### ダッシュボード用

```go
// GetAnalysisData は曜日・時間帯別の集計データを返す（JST 換算）
// AnalysisRow は {DayOfWeek, HourOfDay, MinuteOfHour, AvgViewers, MaxViewers, DataPoints} を持つ
func (d *DB) GetAnalysisData() ([]*AnalysisRow, error)
```

---

## 6. 実装詳細

### New() の初期化フロー

SQLite は並行で書き込みが発生した際に `database is locked` エラーとなりやすいため、Go版では以下の堅牢な初期化設定を行う。
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

Python 版は `datetime.now(timezone.utc).isoformat()` で保存している。
Go 版では以下の形式を使用する：

```go
time.Now().UTC().Format(time.RFC3339)
// 例: "2024-01-15T10:30:00Z"
```

> **注意**: Python 版は `+00:00` 末尾、Go 版は `Z` 末尾になる場合がある。
> SQLite の `strftime` は両方を正しく解釈するため互換性は問題なし。

### GetMovieSummary SQL（Python 版互換の複雑なサブクエリ）

```sql
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
    SUM(comment_delta)              AS total_comments_observed,
    MIN(recorded_at)                AS first_record,
    MAX(recorded_at)                AS last_record
FROM snapshots sn
JOIN sessions s ON sn.session_id = s.id
WHERE s.movie_id = ? AND sn.is_live = 1
```

---

## 7. Python 版との差分

| 項目 | Python 版 | Go 版 |
|---|---|---|
| ドライバー | `sqlite3` (標準ライブラリ) | `modernc.org/sqlite` (pure Go) |
| 接続管理 | `contextmanager` + 毎回接続 | `*sql.DB` (接続プール) |
| 行型 | `sqlite3.Row` (dict-like) | 専用の Go 構造体 |
| トランザクション | `conn.commit()` / `conn.rollback()` | `database/sql` の `Tx` |
