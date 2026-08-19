# TwitCasting Data Collector — Go版 設計概要

## 1. プロジェクト概要

TwitCasting.tv の特定配信を定期ポーリングして、同時視聴者数・コメント数・累計視聴数などのデータを自動収集する CLI ツールの Go 実装。

Python 版（`../python`）を Go で書き直したもの。機能・動作は Python 版と等価とする。

---

## 2. 機能一覧

| コマンド | 説明 |
|---|---|
| `status <user_id>` | 指定ユーザーが配信中なら配信情報（Movie IDやタイトル等）を表示 |
| `watch <user_id>` | 指定ユーザーの監視を開始（オフライン時は -w オプションで待機可能） |
| `sessions` | 過去の監視セッション一覧を表示 |
| `summary <movie_id>` | セッションの統計サマリーを表示 |
| `dashboard` | 収集データをブラウザで確認できる WebUI を起動 |
| `config set` | 認証情報の設定 |
| `config show` | 設定の確認（--verify フラグでAPI接続テスト対応） |

---

## 3. アーキテクチャ概要

```
cmd/tc-analyzer/
    main.go             ← エントリーポイント

internal/
    config/             ← 設定管理 (TOML + 環境変数)
    api/                ← TwitCasting API v2 クライアント
    db/                 ← SQLite データアクセス層
    monitor/            ← 監視ループ・待機ロジック
    dashboard/          ← HTTP サーバー + 組み込みフロントエンド
    cli/                ← CLI コマンド定義 (cobra)
```

---

## 4. 技術スタック

| 分野 | Python 版 | Go 版 |
|---|---|---|
| CLI フレームワーク | typer | cobra (github.com/spf13/cobra) |
| 設定管理 | tomllib / tomli | **viper (github.com/spf13/viper)** + BurntSushi/toml |
| TUI / 表示 | rich | **uilive (github.com/gosuri/uilive)** + lipgloss |
| HTTP クライアント | httpx (async) | net/http (標準ライブラリ) + **golang.org/x/time/rate** (Limiter) |
| SQLite | sqlite3 (標準) | modernc.org/sqlite (pure Go, CGO不要) |
| HTTP サーバー | FastAPI + uvicorn | net/http 標準ライブラリ |
| 非同期 | asyncio | goroutine + channel |

> **Note**: `modernc.org/sqlite` は CGO 不要の pure Go 実装。Windows 環境でのビルドが容易。
> **Note**: レート制限と堅牢な設定ファイル連携、ちらつきのない画面更新のために、Viper、x/time/rate、uilive を導入します。

---

## 5. ディレクトリ構成（案）

```
go/
├── design/                  ← 本設計書群
│   ├── 00_overview.md
│   ├── 01_config.md
│   ├── 02_api_client.md
│   ├── 03_database.md
│   ├── 04_monitor.md
│   ├── 05_cli.md
│   └── 06_dashboard.md
├── cmd/
│   └── tc-analyzer/
│       └── main.go
├── internal/
│   ├── config/
│   │   └── config.go
│   ├── api/
│   │   └── client.go
│   ├── db/
│   │   └── db.go
│   ├── monitor/
│   │   └── monitor.go
│   ├── dashboard/
│   │   ├── server.go
│   │   └── frontend.go  (組み込みHTML)
│   └── cli/
│       ├── root.go
│       ├── watch.go
│       ├── sessions.go
│       ├── summary.go
│       ├── export.go
│       ├── status.go
│       ├── dashboard.go
│       └── config.go
├── go.mod
└── go.sum
```

---

## 6. データフロー

```
ユーザー
  │ CLI コマンド
  ▼
cli/ (cobra)
  │ 設定読み込み
  ▼
config/ → ~/.config/tc-analyzer/config.toml + 環境変数
  │
  ├── api/ → TwitCasting API v2 (HTTPS)
  │     └── MovieSnapshot (構造体)
  │
  ├── db/  → SQLite (~/.config/tc-analyzer/data.db)
  │     ├── sessions テーブル
  │     └── snapshots テーブル
  │
  └── monitor/ → ポーリングループ (goroutine)
        └── MonitorMovie()   → api/ → db/

dashboard/ → net/http → ブラウザ (Chart.js, CSS)
```

---

## 7. 設計方針・注意事項

1. **Python 版との機能等価性**: Python 版と同じ CLI インターフェース（コマンド名・フラグ名）を維持する。
2. **DB 互換性**: SQLite のスキーマは Python 版と完全に同一にする。Python 版で収集したデータも Go 版で参照可能とする。
3. **CGO フリー**: クロスコンパイルを容易にするため CGO に依存しない (`modernc.org/sqlite`)。
4. **シグナルハンドリング**: `Ctrl+C` でグレースフルシャットダウン（監視ループを安全に終了）。context.Context を各層に渡す。
5. **エラーハンドリング**: Go の慣習に従い `error` 値を返す。API エラーは専用の `APIError` 型でラップ。
6. **レートリミット**: TwitCasting API は 60 req/60 sec。Go版ではすべてのデフォルトポーリング間隔を 10 秒とする。
7. **ログ/表示**: Python 版 rich に相当するリッチな出力は ANSI エスケープコードまたは `bubbletea` を検討。基本は `fmt.Fprintf` + ANSI カラーで実装する。

---

## 8. ビルド・実行方法（想定）

```bash
# 依存取得
go mod tidy

# ビルド
go build -o tc-analyzer ./cmd/tc-analyzer

# 実行
./tc-analyzer watch twitcasting_jp
./tc-analyzer watch twitcasting_jp -w
./tc-analyzer dashboard --port 8080
```

---

## 9. 関連ドキュメント

| ファイル | 内容 |
|---|---|
| [01_config.md](./01_config.md) | 設定管理の詳細設計 |
| [02_api_client.md](./02_api_client.md) | API クライアントの詳細設計 |
| [03_database.md](./03_database.md) | SQLite DB 操作の詳細設計 |
| [04_monitor.md](./04_monitor.md) | 監視ループの詳細設計 |
| [05_cli.md](./05_cli.md) | CLI コマンドの詳細設計 |
| [06_dashboard.md](./06_dashboard.md) | ダッシュボードの詳細設計 |
