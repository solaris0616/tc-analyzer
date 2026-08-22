# Project conventions

## Sources of truth

- 実装と`design/*.md`は同じ現在の仕様を表す。
- 設計資料には現在の仕様だけを記述し、変更履歴や他実装との比較はGitに任せる。
- 関数例、SQL、構造体、APIルートを掲載する場合は、実装と全フィールド・全引数を照合する。

## Go and mise

- Goを直接探したり、システムGoへフォールバックしたりせず、`mise`タスクを使う。
- 依存取得: `mise run setup`
- フォーマット: `mise run fmt`
- 通常テスト: `mise run test`
- キャッシュなしテスト: `mise run test-uncached`
- 静的解析: `mise run vet`
- ビルド: `mise run build`
- 全工程ゲート: `mise run verify`
- miseタスクはWindows、macOS、Linuxで同じように実行できるようにする。
- 工程ゲートはGo製の`.codex/tools/project-harness`へ実装し、特定OSのシェルやスクリプト言語へ依存させない。
- 外部コマンドは引数配列で起動し、シェル固有のパイプ、リダイレクト、コマンド連結をmiseタスクへ含めない。

## Persistence invariants

- `sessions`、`snapshots`、`commenters`、`comment_logs`の正本は`internal/db/db.go`のスキーマと`design/03_database.md`。
- `snapshots`にはライブ中の基礎値だけを保存する。オフライン応答は終了判定に使用し、保存しない。
- `interval_sec`、`elapsed_sec`、`is_live`、`comment_delta`は保存しない。
- コメント増分は`comment_count`から`LAG()`でセッション単位に導出する。減少は0として扱い、その後の正の差分だけを加算する。
- コメントIDの重複はログにも投稿者集計にも二重反映しない。
- DBファイルの削除・再作成は、ユーザーが対象を明示した場合だけ行う。

## Cross-surface change checklist

保存形式または公開データ型を変更したら、少なくとも以下を確認する。

1. `internal/db/db.go`: スキーマ、Go型、INSERT、SELECT、Scan、集計SQL
2. `internal/monitor`: 保存前の状態判定、キャンセル、コメント収集
3. `internal/cli`: sessions、summary、export、補完コマンド
4. `internal/dashboard`: レスポンス型、ルート、JavaScript、HTML
5. `design/`: スキーマ、シグネチャ、SQL、API表、処理順
6. テスト: 新規DBスキーマ、正常系、拒否条件、組み合わせ条件

## Event ordering

- 終了条件は、配信者ID不一致などの継続可能な警告条件より先に評価する。
- DB保存、タイトル更新、コールバックより前にオフライン判定を行う。
- コメント収集goroutineは監視終了時にキャンセルし、終了を待つ。

## Working tree

- ユーザーの未コミット変更を保持し、無関係な差分を変更しない。
- フォーマットはGoソースだけに限定する。
- `git diff --check`を最終検証へ含める。
