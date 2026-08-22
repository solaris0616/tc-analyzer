# Agent index

このファイルは索引です。詳細なルールをここへ重複させず、作業内容に応じて以下を読んでください。

## 必ず読むもの

- ソフトウェア変更を行う場合: [開発サイクル](.codex/instructions/development-cycle.md)
- 実装・設計・テストを扱う場合: [プロジェクト規約](.codex/instructions/project-conventions.md)
- 独立レビューを依頼・実施する場合: [レビュー基準](.codex/instructions/independent-review.md)

## スキル

- 要求から最終レビューまで進める: [.codex/skills/tc-development-cycle/SKILL.md](.codex/skills/tc-development-cycle/SKILL.md)
- SQLiteスキーマや保存形式を変更する: [.codex/skills/tc-schema-change/SKILL.md](.codex/skills/tc-schema-change/SKILL.md)
- 工程ゲートまたは最終レビューを行う: [.codex/skills/tc-independent-review/SKILL.md](.codex/skills/tc-independent-review/SKILL.md)

## エージェント

- 独立レビュー担当: [.codex/agents/independent-reviewer.toml](.codex/agents/independent-reviewer.toml)
- プロジェクト内の登録: [.codex/config.toml](.codex/config.toml)

各レビュー工程では新しい`independent_reviewer`を起動してください。同じエージェントに設計・実装とレビューを兼務させないでください。

## 設計資料

- 全体構成: [design/00_overview.md](design/00_overview.md)
- 設定: [design/01_config.md](design/01_config.md)
- TwitCasting API: [design/02_api_client.md](design/02_api_client.md)
- SQLiteと集計: [design/03_database.md](design/03_database.md)
- 監視・コメント収集: [design/04_monitor.md](design/04_monitor.md)
- CLI: [design/05_cli.md](design/05_cli.md)
- ダッシュボード: [design/06_dashboard.md](design/06_dashboard.md)
- 配信者別分析: [design/07_broadcaster_dashboard.md](design/07_broadcaster_dashboard.md)

設計資料は現在の実装を表す正本です。変更履歴や比較説明は含めないでください。

## コマンド

コマンドの正本は[mise.toml](mise.toml)です。通常は次を使用します。

- フォーマット: `mise run fmt`
- キャッシュなしテスト: `mise run test-uncached`
- 静的解析: `mise run vet`
- ビルド: `mise run build`
- 工程ゲート一括検証: `mise run verify`
