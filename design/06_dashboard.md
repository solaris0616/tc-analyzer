# 設計書 06 — 可視化ダッシュボードサーバー (`internal/dashboard`)

## 1. 概要

Python 版 `dashboard.py` (FastAPI + uvicorn) に相当する可視化ダッシュボードの Web サーバ。
Go 版では、外部の重量フレームワークを使用せず、標準ライブラリ `net/http` と Go 1.16 で導入された `embed` パッケージを活用して、軽量かつシングルバイナリで動作するダッシュボードサーバーを構築する。

---

## 2. API エンドポイント設計

提供する API およびルーティングは Python 版と完全に互換性を持たせる。

| パス | メソッド | 説明 |
|---|---|---|
| `/` | `GET` | 埋め込みフロントエンド HTML の提供 |
| `/api/movies` | `GET` | 監視された配信 (Movie ID 単位) の一覧取得 |
| `/api/movies/{movie_id}` | `GET` | 指定された Movie ID の詳細・集計サマリー・スナップショット群の取得 |
| `/api/analysis` | `GET` | 全データに基づく曜日・時間帯別集計データ (JST) の取得 |

---

## 3. Go 1.16 `//go:embed` による静的アセット埋め込み

フロントエンドコード（HTML/CSS/JS）は、Go の `embed` 機能を利用してバイナリに静的埋め込みする。これにより、Python 版の「巨大な文字列定数」によるコード視認性悪化を防ぎつつ、配布用のシングルバイナリ性を維持する。

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
    mux.HandleFunc("GET /api/movies", s.handleGetMovies)
    mux.HandleFunc("GET /api/movies/{movie_id}", s.handleGetMovieDetail)
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
type MovieResponse struct {
    MovieID      string    `json:"movie_id"`
    StartedAt    string    `json:"started_at"` // RFC3339 形式
    Label        string    `json:"label"`
    IntervalSec  int       `json:"interval_sec"`
    TotalRecords int       `json:"total_records"`
}
```

### 5.2. `/api/movies/{movie_id}` (GET)

```go
type MovieDetailResponse struct {
    Movie     *db.Session        `json:"movie"`
    Summary   *db.SessionSummary `json:"summary"`
    Snapshots []*db.Snapshot     `json:"snapshots"`
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

データベースクエリは、Python 版の SQLite 特有のタイムゾーンオフセット SQL をそのまま再現する：
```sql
SELECT 
    CAST(strftime('%w', datetime(recorded_at, '+9 hours')) AS INTEGER) AS day_of_week,
    CAST(strftime('%H', datetime(recorded_at, '+9 hours')) AS INTEGER) AS hour_of_day,
    CASE WHEN CAST(strftime('%M', datetime(recorded_at, '+9 hours')) AS INTEGER) < 30 THEN 0 ELSE 30 END AS minute_of_hour,
    AVG(current_view_count) AS avg_viewers,
    MAX(current_view_count) AS max_viewers,
    COUNT(*) AS data_points
FROM snapshots
WHERE is_live = 1
GROUP BY day_of_week, hour_of_day, minute_of_hour
ORDER BY day_of_week, hour_of_day, minute_of_hour;
```

---

## 6. フロントエンド実装の修正ポイント

Python 版の HTML を Go 埋め込みに移行する際、以下の小修正を加える：
- フロントエンドにおける日時表示 (UTC -> JST やフォーマット調整) でのパース互換性の確保。
- Chart.js の CDN 読み込み、およびグラフのレスポンシブオプションの維持。

---

## 7. Python 版との比較・主要な設計差分

| 調査項目 | Python 版 (FastAPI) | Go 版 (net/http) |
|---|---|---|
| Web フレームワーク | `FastAPI` (Pydantic 自動検証) | `net/http` (標準Mux) + `encoding/json` |
| ASGI サーバー | `uvicorn` | 標準 `http.Server.ListenAndServe()` |
| フロントエンド埋め込み | 文字列定数としてのインライン定義 | `//go:embed` による外部ファイル結合 |
| ルーティング定義 | アノテーションによる定義 | `http.ServeMux` パターンマッチング |
| JSON 変換エラー | 422 Unprocessable Entity (自動) | 自前でデコード、400 Bad Request 返却 |
