# 設計書 07 — 配信者別ダッシュボード

## 1. 目的

ダッシュボード上のデータを配信者単位で分離する。

- ダッシュボード上部で配信者を選択できる
- サイドバーには、選択した配信者の配信（分析対象）のみを表示する
- 「曜日別平均同時視聴者数」は、選択した配信者に属する全計測セッションのライブ中スナップショットだけから算出する
- 新規収集データだけでなく、既存DBのデータも可能な範囲で配信者へ帰属させる

本設計では用語を次のように区別する。

| 用語 | 識別子 | 意味 |
|---|---|---|
| 配信者 | `broadcaster_id` | TwitCastingユーザー。選択・集計の境界 |
| 配信 | `movie_id` | 1回のライブ配信。現在のサイドバー表示単位 |
| 計測セッション | `sessions.id` | `watch` 1回の実行。1配信に複数存在し得る |

サイドバーは既存動作を維持し、同じ `movie_id` の複数計測セッションを1件の「配信」として表示する。

## 2. 現状と問題

現在の `sessions` は `movie_id` しか保持していない。APIクライアントは配信者名とscreen IDを取得しているが、DBには保存していない。

`GetAnalysisData()` は `snapshots` 全体に対して `AVG(current_view_count)` を実行しているため、複数配信者のデータが同じ曜日・時間帯バケットに混在する。

また、表示上の「分析セッション一覧」は実際には `movie_id` ごとの配信一覧であり、配信者との関係を判別できない。

## 3. データモデル

### 3.1 `sessions` の拡張

```sql
ALTER TABLE sessions ADD COLUMN broadcaster_id        TEXT;
ALTER TABLE sessions ADD COLUMN broadcaster_screen_id TEXT;
ALTER TABLE sessions ADD COLUMN broadcaster_name      TEXT;

CREATE INDEX IF NOT EXISTS idx_sessions_broadcaster
    ON sessions(broadcaster_id, movie_id, started_at);
```

3列は既存DBとの互換性のためNULLを許可する。新規セッションではすべて必須として扱う。

起動時マイグレーションは `PRAGMA table_info(sessions)` で各列の有無を確認してから、トランザクション内で不足列だけを追加する。現在の `title` 列追加のように `ALTER TABLE` エラーを無視せず、失敗時はDB初期化エラーとして返す。

- `broadcaster_id`: APIレスポンスの `broadcaster.id`。変更されない識別キーとして選択・検索・集計に使う
- `broadcaster_screen_id`: `@screen_id` 表示用。変更され得るため識別キーには使わない
- `broadcaster_name`: 表示名

配信者マスターテーブルは設けない。セッションへ配信時点の表示情報を保存することで、名前変更後も履歴を保持できる。配信者一覧では、同じ `broadcaster_id` の最新セッションにある表示情報を採用する。

### 3.2 Go型

`api.MovieSnapshot` に現在捨てている `BroadcasterID` を追加する。

`db.Session` に以下を追加する。

```go
BroadcasterID       string `json:"broadcaster_id"`
BroadcasterScreenID string `json:"broadcaster_screen_id"`
BroadcasterName     string `json:"broadcaster_name"`
```

配信者セレクター用に次の型を追加する。

```go
type BroadcasterListRow struct {
    ID           string    `json:"id"`
    ScreenID     string    `json:"screen_id"`
    Name         string    `json:"name"`
    MovieCount   int       `json:"movie_count"`
    SessionCount int       `json:"session_count"`
    LastSeenAt   time.Time `json:"last_seen_at"`
}
```

## 4. 収集フロー

`MonitorMovie` は配信開始確認時に得たスナップショットから、配信者ID・screen ID・名前を `CreateSession` へ渡す。待機モードは現在 `waitForLive` からmovie IDしか返らないため、待機終了後かつセッション作成前に `GetMovieInfo(movieID)` を1回実行して配信者情報を確定する。

```go
CreateSession(movieID string, intervalSec int, label string, broadcaster Broadcaster) (int64, error)
```

セッション作成前に配信者IDが得られなければ開始をエラーにする。配信者が不明な新規データを増やさず、異なる配信者が混ざる可能性を入口で防ぐ。

ポーリング中に取得した配信者表示情報が変わった場合は、そのセッションのscreen IDと名前だけを更新してよい。`broadcaster_id` がセッション作成時と異なる場合はデータを保存せずエラーとして記録する。

## 5. DBクエリ

### 5.1 配信者一覧

`ListBroadcasters()` は `broadcaster_id IS NOT NULL` のセッションを配信者IDでグループ化し、配信数、計測セッション数、最終計測日時を返す。表示名とscreen IDは配信者ごとの最新セッションから取得する。

配信者未設定の既存データは通常の配信者と混ぜず、一覧末尾に「配信者不明」として件数を表示する。ただし選択時に複数配信者の可能性があるデータを集計しない。

### 5.2 配信一覧

`ListMoviesByBroadcaster(broadcasterID string)` を追加する。現在の `ListMovies()` と同じく `movie_id` ごとにまとめるが、すべてのCTEに次の条件を適用する。

```sql
WHERE s.broadcaster_id = ?
```

### 5.3 曜日別平均同時視聴者数

`GetAnalysisData(broadcasterID string)` に変更し、必ず配信者IDを要求する。

```sql
SELECT
    CAST(strftime('%w', datetime(sn.recorded_at, '+9 hours')) AS INTEGER) AS day_of_week,
    CAST(strftime('%H', datetime(sn.recorded_at, '+9 hours')) AS INTEGER) AS hour_of_day,
    CASE
        WHEN CAST(strftime('%M', datetime(sn.recorded_at, '+9 hours')) AS INTEGER) < 30
        THEN 0 ELSE 30
    END AS minute_of_hour,
    AVG(sn.current_view_count) AS avg_viewers,
    MAX(sn.current_view_count) AS max_viewers,
    COUNT(*) AS data_points
FROM snapshots sn
JOIN sessions s ON s.id = sn.session_id
WHERE sn.is_live = 1
  AND s.broadcaster_id = ?
GROUP BY day_of_week, hour_of_day, minute_of_hour
ORDER BY day_of_week, hour_of_day, minute_of_hour;
```

平均の定義は既存仕様を維持し、「選択配信者の全計測セッションに含まれるライブ中スナップショットの標本平均」とする。したがって、計測時間が長いセッションや計測間隔が短いセッションほど寄与が大きい。セッションごとの等加重平均への変更は別要件とする。

## 6. Dashboard API

| パス | 必須パラメータ | 説明 |
|---|---|---|
| `GET /api/broadcasters` | なし | 配信者一覧 |
| `GET /api/movies?broadcaster_id={id}` | `broadcaster_id` | 選択配信者の配信一覧 |
| `GET /api/movies/{movie_id}` | なし | 従来どおり、1配信の詳細 |
| `GET /api/movies/{movie_id}/commenters` | なし | 従来どおり、1配信のコメントユーザー |
| `GET /api/analysis?broadcaster_id={id}` | `broadcaster_id` | 選択配信者だけの曜日・時間帯別集計 |

`/api/movies` と `/api/analysis` はパラメータなしの場合に `400 Bad Request` を返す。誤って全配信者を混ぜた結果を返さないことを、UIだけでなくAPI境界でも保証する。

`broadcaster_id` はURLクエリ値として `url.QueryEscape` 相当でエンコードする。存在しないIDは空配列ではなく `404 Not Found` とし、古い選択状態をフロントエンドが検知できるようにする。

## 7. UI状態と画面遷移

### 7.1 配置

トップヘッダーに「配信者」セレクターを追加する。選択肢は次の形式で表示する。

```text
表示名 (@screen_id) — 12配信
```

グラフの副題は「選択中の配信者の全配信データから集計」に変更し、現在の集計範囲を明示する。

### 7.2 状態

```text
currentBroadcasterID
  ├─ movie list: /api/movies?broadcaster_id=...
  └─ analysis:   /api/analysis?broadcaster_id=...
       └─ currentMovieID: /api/movies/{movie_id}
```

初期表示では、前回選択した配信者IDを `localStorage` から復元する。存在しない場合は最終計測日時が最も新しい配信者を選ぶ。

配信者を変更したときは次の順で更新する。

1. `currentMovieID` とコメントユーザーキャッシュを破棄する
2. 配信一覧と曜日別分析を、新しい配信者ID付きで並行取得する
3. 配信一覧の先頭を選択し、配信詳細を取得する
4. 0件なら詳細カードとグラフを空状態にする

自動更新時も `currentBroadcasterID` を維持する。切り替え中の古いレスポンスで画面を上書きしないよう、リクエスト世代番号または `AbortController` で競合を防止する。

## 8. 既存データの移行

DBスキーマ変更だけでは、既存セッションの配信者を推測できない。`movie_id` ごとにTwitCasting APIの `GET /movies/{movie_id}` を呼ぶバックフィル処理を追加する。

```text
tc-analyzer sessions backfill-broadcasters
```

処理仕様:

1. `broadcaster_id IS NULL OR broadcaster_id = ''` の異なる `movie_id` を列挙する
2. APIから配信者ID・screen ID・名前を取得する
3. 同じ `movie_id` の全セッションを1トランザクションで更新する
4. 成功済みの配信はスキップし、何度でも安全に再実行できる
5. APIエラーはmovie ID単位で記録し、残りを継続する
6. 最後に成功・失敗・未分類件数を表示する

ダッシュボード起動時には自動バックフィルしない。起動が外部APIの状態に左右されることと、閲覧操作が暗黙にDBを変更することを避ける。

バックフィル未実行または取得失敗のデータは「配信者不明」として可視化するが、曜日別平均には含めない。これにより、不完全な移行中でも既知の配信者同士が混在しない。

## 9. バリデーションとエラー表示

- 配信者一覧0件: セレクターを無効化し、バックフィルまたは新規収集を案内する
- 選択配信者の配信0件: サイドバーと詳細領域を空状態にする
- 配信者一覧取得失敗: 既存表示を保持し、ヘッダーに取得エラーを表示する
- 配信一覧または分析取得失敗: 該当領域だけエラー表示し、別領域の更新は継続する
- `broadcaster_id` なしの分析API呼び出し: 400
- 不明な `broadcaster_id`: 404

## 10. テスト方針

### DB

- 新規セッションに配信者3項目が保存・取得される
- 2配信者、複数movie、複数sessionを作り、配信一覧が選択配信者だけになる
- 曜日別平均が選択配信者のライブ中スナップショットだけで計算される
- `is_live = 0` と配信者未設定のスナップショットが平均から除外される
- 最新セッションの配信者表示情報が一覧に使われる
- 旧スキーマのDBを開くと列とインデックスが追加され、既存行は保持される

### Monitor / API

- APIレスポンスの `broadcaster.id` が `MovieSnapshot` にマッピングされる
- `MonitorMovie` がセッション作成時に配信者情報を保存する
- ポーリング途中で配信者IDが変化した場合に保存を拒否する

### Dashboard API

- 配信者一覧の件数と並び順
- `/api/movies` と `/api/analysis` の配信者フィルター
- 必須パラメータなしの400、不明IDの404
- レスポンスに別配信者のデータが含まれない

### Frontend

- 初期選択、選択変更、前回選択の復元
- 配信者変更時に配信一覧・詳細・曜日別グラフがすべて切り替わる
- 自動更新後も選択配信者を維持する
- 高速な連続選択で古いレスポンスが画面を上書きしない

## 11. 実装順序

1. API型へ `BroadcasterID` を追加
2. DBマイグレーション、型、配信者別クエリを追加
3. Monitorから配信者情報を保存
4. バックフィルコマンドを追加
5. Dashboard APIを配信者必須へ変更
6. 配信者セレクターとフロントエンド状態管理を追加
7. DB・Monitor・API・UIのテストを追加し、設計書03/04/05/06を更新

## 12. 完了条件

- 画面上で各保存データの配信者を識別できる
- 配信者変更直後と自動更新後の両方で、サイドバーに別配信者の配信が混ざらない
- 曜日別平均のSQLが必ず `broadcaster_id` で絞り込まれ、別配信者および配信者不明データを含まない
- 既存DBを破壊せず移行でき、バックフィルは中断後も再実行できる
