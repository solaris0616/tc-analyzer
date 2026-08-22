---
name: tc-schema-change
description: tc-analyzerのSQLiteスキーマ、保存カラム、集計SQL、DB由来のAPIやエクスポート形式を変更する。
---

# tc-analyzer schema change

最初に以下を読む。

- `.codex/instructions/project-conventions.md`
- `design/03_database.md`
- 関連する`design/04_monitor.md`、`design/05_cli.md`、`design/06_dashboard.md`

変更前に、保存する基礎値と導出値を分離する。導出可能な値を追加保存する場合は、その必要性と不整合防止策を設計に明記する。

次を一単位で変更する。

1. CREATE TABLEと索引
2. GoのDB型
3. INSERT、SELECT、Scan、集計SQL
4. 監視時の保存前判定
5. CLI表示とCSV/JSONエクスポート
6. ダッシュボードレスポンス、JavaScript、HTML
7. 全関連設計資料
8. 新規DBのスキーマ検査と境界条件テスト

SQLの時系列差分には順序を一意にするキーを含め、複数セッションを必ず分割する。カウンター減少、重複、空データ、複数セッションをテストする。

開発中のため、ユーザーが求めていないデータ変換処理や追加レイヤーを推測で実装しない。DBの削除や再作成は、ユーザーが対象を明示した場合だけ行う。
