---
name: tc-development-cycle
description: tc-analyzerで機能追加、修正、リファクタリング、設計変更を、要求整理から工程別独立レビューと最終検証まで進める。
---

# tc-analyzer development cycle

ソフトウェア変更では、最初に次を読む。

- `AGENTS.md`
- `.codex/instructions/development-cycle.md`
- `.codex/instructions/project-conventions.md`
- 変更対象に対応する`design/*.md`

要求整理、設計、実装、テスト、最終確認を順に進める。設計・実装・テストの各工程完了後と最終確認時に、それぞれ新しい`independent_reviewer`を起動する。レビュー担当には編集させない。

妥当な指摘は修正し、影響した工程の検証と新しい独立レビューを繰り返す。「全指摘クリア」と`mise run verify`の成功が揃うまで完了扱いにしない。

DBまたは保存形式を変更する場合は、追加で`.codex/skills/tc-schema-change/SKILL.md`を読む。

