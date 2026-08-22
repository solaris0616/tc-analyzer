# 設計書 06 — 可視化ダッシュボードサーバー (`internal/dashboard`)

## 1. 概要

標準ライブラリ`net/http`と`embed`パッケージを使用し、軽量かつシングルバイナリで動作する可視化ダッシュボードサーバーを構築する。

---

## 2. API エンドポイント設計

保存スキーマに合わせたAPIとルーティングを提供する。

| パス | メソッド | 説明 |
|---|---|---|
| `/` | `GET` | 埋め込みフロントエンド HTML の提供 |
| `/api/broadcasters` | `GET` | 保存済みデータの配信者一覧取得 |
| `/api/movies?broadcaster_id={id}` | `GET` | 選択配信者の監視された配信一覧取得 |
| `/api/movies/{movie_id}` | `GET` | 指定された Movie ID の詳細・集計サマリー・スナップショット群の取得 |
| `/api/movies/{movie_id}/commenters` | `GET` | 指定されたMovie IDのコメント投稿者一覧取得 |
| `/api/analysis?broadcaster_id={id}` | `GET` | 選択配信者に基づく曜日・時間帯別集計データ (JST) の取得 |

配信者選択、APIの必須パラメータ、画面状態遷移の詳細は[設計書07](07_broadcaster_dashboard.md)を参照する。

---

## 3. Go 1.16 `//go:embed` による静的アセット埋め込み

フロントエンドコード（HTML/CSS/JS）は、`embed`機能を利用してバイナリに静的埋め込みし、配布用のシングルバイナリ性を維持する。

### ディレクトリ構成 (internal/dashboard 配下)

```
internal/dashboard/
  ├── server.go       # サーバ起動・APIハンドラロジック
  └── assets/
        ├── index.html  # フロントエンド HTML
        ├── app.js      # Chart.js マウント・データ取得ロジック
        └── style.css   # スタイル
```

### server.go での定義

```go
package dashboard

import (
    "embed"
    "net/http"
)

//go:embed assets
var frontendAssets embed.FS

// 静的アセットの配信に http.FileServer(http.FS(...)) を用いることで、
// Go 標準ライブラリがファイル拡張子から MIME タイプを自動判定し、
// Content-Type ヘッダーを適切に設定する。
// ブラウザの MIME チェックによるスクリプト実行拒否を防ぐ。
```

---

## 4. ルーティングと API ハンドラ

Go 1.22 以降の標準 `http.ServeMux` (ワイルドカードマッチング対応) を使用して、パスパラメータを解析する。

また、`db.DB`（SQLite）は Go のレベルで接続数が 1 に制限 (`SetMaxOpenConns(1)`) されているため、複数の HTTP リクエストによる並行読み込みクエリは DB クライアントによって内部で適切に直列化され、`database is locked` を防ぎ安全に処理される。

```go
func NewServer(dbClient *db.DB, host string, port int) *Server {
    return &Server{
        db:   dbClient,
        addr: net.JoinHostPort(host, strconv.Itoa(port)),
    }
}

type Server struct {
    db   *db.DB
    addr string
}

func (s *Server) Start(ctx context.Context) error {
    mux := http.NewServeMux()

    // フロントエンド配信： http.FileServer(http.FS(...)) により正しい MIME タイプを自動設定
    // 拡張子に応じて Content-Type: text/html, application/javascript, text/css 等を自動適用
    staticFS, _ := fs.Sub(frontendAssets, "assets")
    mux.Handle("GET /", http.FileServer(http.FS(staticFS)))

    // REST API
    mux.HandleFunc("GET /api/broadcasters", s.handleGetBroadcasters)
    mux.HandleFunc("GET /api/movies", s.handleGetMovies)
    mux.HandleFunc("GET /api/movies/{movie_id}", s.handleGetMovieDetail)
    mux.HandleFunc("GET /api/movies/{movie_id}/commenters", s.handleGetCommenters)
    mux.HandleFunc("GET /api/analysis", s.handleGetAnalysis)

    server := &http.Server{
        Addr:    s.addr,
        Handler: loggerMiddleware(mux),
    }

    // Graceful Shutdown の制御
    go func() {
        <-ctx.Done()
        shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        server.Shutdown(shutdownCtx)
    }()

    log.Printf("Starting dashboard server on http://%s\n", s.addr)
    if err := server.ListenAndServe(); err != http.ErrServerClosed {
        return err
    }
    return nil
}
```

---

## 5. API レスポンスと型定義

JSON レスポンスのシリアライズ用に構造体を定義する。

### 5.1. `/api/movies` (GET)

```go
type MovieListRow struct {
    MovieID      string    `json:"movie_id"`
    StartedAt    time.Time `json:"started_at"`
    Label        string    `json:"label"`
    TotalRecords int       `json:"total_records"`
    Title        string    `json:"title"`
}
```

### 5.2. `/api/movies/{movie_id}` (GET)

```go
type MovieDetailResponse struct {
    Movie          *db.Session        `json:"movie"`
    Summary        *db.SessionSummary `json:"summary"`
    Snapshots      []*db.Snapshot     `json:"snapshots"`
    CommenterCount int                `json:"commenter_count"`
}
```

### 5.3. `/api/analysis` (GET)

曜日・時間帯別集計 (JST 換算) のレスポンス構造。

```go
type AnalysisRow struct {
    DayOfWeek    int     `json:"day_of_week"`     // strftime('%w') 0-6
    HourOfDay    int     `json:"hour_of_day"`     // strftime('%H') 0-23
    MinuteOfHour int     `json:"minute_of_hour"`  // 0 or 30 (30分単位のグループ化)
    AvgViewers   float64 `json:"avg_viewers"`
    MaxViewers   int     `json:"max_viewers"`
    DataPoints   int     `json:"data_points"`
}
```

データベースクエリは、SQLiteの日時関数でJSTへ変換する：
```sql
SELECT 
    CAST(strftime('%w', datetime(sn.recorded_at, '+9 hours')) AS INTEGER) AS day_of_week,
    CAST(strftime('%H', datetime(sn.recorded_at, '+9 hours')) AS INTEGER) AS hour_of_day,
    CASE WHEN CAST(strftime('%M', datetime(sn.recorded_at, '+9 hours')) AS INTEGER) < 30 THEN 0 ELSE 30 END AS minute_of_hour,
    AVG(sn.current_view_count) AS avg_viewers,
    MAX(sn.current_view_count) AS max_viewers,
    COUNT(*) AS data_points
FROM snapshots sn
JOIN sessions s ON s.id = sn.session_id
WHERE s.broadcaster_id = ?
GROUP BY day_of_week, hour_of_day, minute_of_hour
ORDER BY day_of_week, hour_of_day, minute_of_hour;
```

---

## 6. フロントエンド実装

フロントエンドではUTC日時を表示用タイムゾーンへ変換し、Chart.jsでレスポンシブなグラフを描画する。
