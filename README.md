# tc-analyzer (TwitCasting Data Collector - Go版)

TwitCasting.tv の特定配信を定期ポーリングし、同時視聴者数・コメント数・累計視聴数などのデータを自動収集・可視化する CLI ツールです。

## 主な機能

- **配信ステータス確認 (`status`)**: 指定ユーザーの現在の配信状況を確認
- **配信監視・データ収集 (`watch`)**: 配信をポーリングしてメトリクスを SQLite に記録（配信開始待機モード `-w` あり）
- **セッション履歴一覧 (`sessions`)**: 過去の監視セッション情報を一覧表示
- **セッション統計サマリー (`summary`)**: 特定配信の統計（最大・平均同時視聴数など）を表示
- **Web ダッシュボード (`dashboard`)**: 収集データをブラウザでグラフ表示
- **データエクスポート (`export`)**: 収集データを CSV または JSON で出力
- **設定管理 (`config`)**: API 認証情報（Client ID / Client Secret）や既定値を管理

## 必要要件

- Go 1.26.5 以上

## インストール・ビルド

```bash
# 依存関係のダウンロード
go mod download

# バイナリのビルド
go build -o tc-analyzer ./cmd/tc-analyzer
```

## クイックスタート

### 1. API 認証情報の設定

TwitCasting API の認証情報を設定します。

```bash
./tc-analyzer config set --client-id <CLIENT_ID> --client-secret <CLIENT_SECRET>

# 設定確認・接続テスト
./tc-analyzer config show --verify
```

### 2. 配信の監視・データ収集

```bash
# 現在配信中のライブを監視
./tc-analyzer watch <user_id>

# オフラインの場合でも配信開始まで待機して監視
./tc-analyzer watch <user_id> -w
```

### 3. ダッシュボードの起動

```bash
./tc-analyzer dashboard --port 8080
```

ブラウザで <http://localhost:8080> を開くと、グラフやセッション情報を確認できます。`--port` を省略した場合のポートは 8000 です。

## ディレクトリ構成

```text
cmd/tc-analyzer/     エントリーポイント
internal/
  api/               TwitCasting API v2 クライアント
  cli/               Cobra CLI コマンド定義
  config/            設定管理
  dashboard/         Web ダッシュボード（HTTP サーバーと組み込みフロントエンド）
  db/                SQLite データベース操作
  monitor/           監視ループ・待機ロジック
design/              詳細設計ドキュメント群
```

## ライセンス

MIT License
