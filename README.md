# tc-analyzer

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

- [mise](https://mise.jdx.dev/)

Go は `mise.toml` でバージョンを固定しているため、個別にインストールする必要はありません。

## セットアップ

```bash
# mise.toml に定義された Go をインストール
mise install

# Go モジュールの依存関係をダウンロード
mise run setup
```

利用可能なタスクは次のコマンドで確認できます。

```bash
mise tasks ls
```

## 開発コマンド

```bash
# ソースコードのフォーマット
mise run fmt

# テスト
mise run test

# キャッシュなしテスト
mise run test-uncached

# 静的解析
mise run vet

# テストと静的解析をまとめて実行
mise run check

# バイナリのビルド
mise run build

# フォーマット・テスト・静的解析・ビルド・資料の一括検証
mise run verify

# CLI をソースから実行（例: ヘルプ表示）
mise run tc --help
```

Windows では `tc-analyzer.exe`、macOS/Linux では `tc-analyzer` がプロジェクトルートに生成されます。

エージェント向けの索引と開発工程は[AGENTS.md](AGENTS.md)にあります。エージェント、スキル、工程別インストラクション、クロスプラットフォームな検証ツールは`.codex/`配下で管理します。

検証ツールはGoで実装し、CIではUbuntu上で`mise run verify`を実行します。

## クイックスタート

### 1. API 認証情報の設定

TwitCasting API の認証情報を設定します。

```bash
mise run tc config set --client-id <CLIENT_ID> --client-secret <CLIENT_SECRET>

# 設定確認・接続テスト
mise run tc config show --verify
```

### 2. 配信の監視・データ収集

```bash
# 現在配信中のライブを監視
mise run tc watch <user_id>

# オフラインの場合でも配信開始まで待機して監視
mise run tc watch <user_id> -w
```

### 3. ダッシュボードの起動

```bash
mise run tc dashboard --port 8080
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
mise.toml            Go バージョンと開発タスクの定義
```

## ライセンス

MIT License
