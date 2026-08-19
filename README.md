# tc-analyzer (TwitCasting Data Collector - Go版)

TwitCasting.tv の特定配信を定期ポーリングし、同時視聴者数・コメント数・累計視聴数などのデータを自動収集・可視化する CLI ツールです。

## 主な機能

- **配信ステータス確認 (status)**: 指定ユーザーの現在の配信状況を確認
- **配信監視・データ収集 (watch)**: 配信をポーリングしてメトリクスをSQLiteに記録（配信開始待機モード -w あり）
- **セッション履歴一覧 (sessions)**: 過去の監視セッション情報を一覧表示
- **セッション統計サマリー (summary)**: 特定セッションの統計（最大・平均同時視聴数など）を表示
- **Webダッシュボード (dashboard)**: 収集データをブラウザでグラフ表示
- **データエクスポート (xport)**: 収集データをCSV/JSON等で出力
- **設定管理 (config)**: API認証情報（ClientId / ClientSecret または AccessToken）の管理

## 必要要件

- Go 1.22 以上

## インストール・ビルド

`ash
# 依存関係のダウンロード
go mod tidy

# バイナリのビルド
go build -o tc-analyzer ./cmd/tc-analyzer
`

## クイックスタート

### 1. API 認証情報の設定
TwitCasting API の認証情報を設定します。

`ash
./tc-analyzer config set --client-id <CLIENT_ID> --client-secret <CLIENT_SECRET>
# または Access Token の場合
./tc-analyzer config set --access-token <ACCESS_TOKEN>

# 設定確認・接続テスト
./tc-analyzer config show --verify
`

### 2. 配信の監視・データ収集
`ash
# 現在配信中のライブを監視
./tc-analyzer watch <user_id>

# オフラインの場合でも配信開始まで待機して監視
./tc-analyzer watch <user_id> -w
`

### 3. ダッシュボードの起動
`ash
./tc-analyzer dashboard --port 8080
`
ブラウザで http://localhost:8080 を開くとグラフやセッション情報を確認できます。

## ディレクトリ構成

- cmd/tc-analyzer/ - エントリーポイント
- internal/
  - pi/ - TwitCasting API v2 クライアント
  - cli/ - Cobra CLI コマンド定義
  - config/ - 設定管理
  - dashboard/ - Webダッシュボード（HTTPサーバー & 組み込みフロントエンド）
  - db/ - SQLite データベース操作
  - monitor/ - 監視ループ・待機ロジック
- design/ - 詳細設計ドキュメント群

## ライセンス

MIT License
