# 設計書 07 — 配信者別ダッシュボード

## 1. 目的

ダッシュボード上のデータを配信者単位で分離する。

- ダッシュボード上部で配信者を選択できる
- サイドバーには、選択した配信者の配信（分析対象）のみを表示する
- 「曜日別平均同時視聴者数」は、選択した配信者に属する全計測セッションのスナップショットから算出する
- 配信者情報が欠けたセッションをAPI情報で補完できる

本設計では用語を次のように区別する。

| 用語 | 識別子 | 意味 |
|---|---|---|
| 配信者 | `broadcaster_id` | TwitCastingユーザー。選択・集計の境界 |
| 配信 | `movie_id` | 1回のライブ配信。現在のサイドバー表示単位 |
| 計測セッション | `sessions.id` | `watch` 1回の実行。1配信に複数存在し得る |

サイドバーでは、同じ `movie_id` の複数計測セッションを1件の「配信」として表示する。

## 2. 集計境界

`sessions`に配信者IDを保存し、配信一覧と時間帯別分析を必ず`broadcaster_id`で絞り込む。これにより、複数配信者のデータが同じ集計へ混在することを防ぐ。

## 3. データモデル

### 3.1 `sessions`

```sql
CREATE TABLE sessions (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    movie_id              TEXT NOT NULL,
    started_at            TEXT NOT NULL,
    label                 TEXT,
    title                 TEXT,
    broadcaster_id        TEXT,
    broadcaster_screen_id TEXT,
    broadcaster_name      TEXT
);

CREATE INDEX IF NOT EXISTS idx_sessions_broadcaster
    ON sessions(broadcaster_id, movie_id, started_at);
```

配信者3列は情報補完操作を可能にするためNULLを許可する。監視処理では配信者IDを必須とし、取得できなければセッションを作成しない。

- `broadcaster_id`: APIレスポンスの `broadcaster.id`。変更されない識別キーとして選択・検索・集計に使う
- `broadcaster_screen_id`: `@screen_id` 表示用。変更され得るため識別キーには使わない
- `broadcaster_name`: 表示名

配信者マスターテーブルは設けない。セッションへ配信時点の表示情報を保存することで、名前変更後も履歴を保持できる。配信者一覧では、同じ `broadcaster_id` の最新セッションにある表示情報を採用する。

### 3.2 Go型

`api.MovieSnapshot`は`BroadcasterID`を保持する。

`db.Session`は以下のフィールドを持つ。

```go
BroadcasterID       string `json:"broadcaster_id"`
BroadcasterScreenID string `json:"broadcaster_screen_id"`
BroadcasterName     string `json:"broadcaster_name"`
```

配信者セレクターには次の型を使用する。

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

`MonitorMovie`は配信開始確認時に得たスナップショットから、配信者ID・screen ID・名前をセッション作成処理へ渡す。

```go
CreateSessionWithBroadcaster(movieID string, label string, broadcaster Broadcaster) (int64, error)
```

セッション作成前に配信者IDが得られなければ開始をエラーにする。配信者が不明な新規データを増やさず、異なる配信者が混ざる可能性を入口で防ぐ。

配信者のscreen IDと名前はセッション作成時の情報を保持する。ポーリング中に`broadcaster_id`がセッション作成時と異なる場合は、スナップショットを保存せず警告を記録する。

## 5. DBクエリ

### 5.1 配信者一覧

`ListBroadcasters()` は `broadcaster_id IS NOT NULL` のセッションを配信者IDでグループ化し、配信数、計測セッション数、最終計測日時を返す。表示名とscreen IDは配信者ごとの最新セッションから取得する。

配信者未設定データは通常の配信者と混ぜず、時間帯別分析にも含めない。

### 5.2 配信一覧

`ListMoviesByBroadcaster(broadcasterID string)`は`movie_id`ごとにまとめ、すべてのCTEに次の条件を適用する。

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
WHERE s.broadcaster_id = ?
GROUP BY day_of_week, hour_of_day, minute_of_hour
ORDER BY day_of_week, hour_of_day, minute_of_hour;
```

平均は「選択配信者の全計測セッションに含まれるスナップショットの標本平均」とする。したがって、計測時間が長いセッションや計測間隔が短いセッションほど寄与が大きい。

## 6. Dashboard API

| パス | 必須パラメータ | 説明 |
|---|---|---|
| `GET /api/broadcasters` | なし | 配信者一覧 |
| `GET /api/movies?broadcaster_id={id}` | `broadcaster_id` | 選択配信者の配信一覧 |
| `GET /api/movies/{movie_id}` | なし | 1配信の詳細 |
| `GET /api/movies/{movie_id}/commenters` | なし | 1配信のコメントユーザー |
| `GET /api/analysis?broadcaster_id={id}` | `broadcaster_id` | 選択配信者だけの曜日・時間帯別集計 |

`/api/movies` と `/api/analysis` はパラメータなしの場合に `400 Bad Request` を返す。誤って全配信者を混ぜた結果を返さないことを、UIだけでなくAPI境界でも保証する。

`broadcaster_id`はURLクエリ値としてエンコードする。存在しないIDは空配列ではなく`404 Not Found`とし、保存済みの選択状態が無効なことをフロントエンドで検知できるようにする。

## 7. UI状態と画面遷移

### 7.1 配置

トップヘッダーに「配信者」セレクターを配置する。選択肢は次の形式で表示する。

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

自動更新時も`currentBroadcasterID`を維持する。切り替え前に開始したレスポンスで画面を上書きしないよう、リクエスト世代番号で競合を防止する。

## 8. 配信者情報の補完

配信者情報がないセッションは、`movie_id`ごとにTwitCasting APIの`GET /movies/{movie_id}`を呼び出して補完できる。

```text
tc-analyzer sessions backfill-broadcasters
```

処理仕様:

1. `broadcaster_id IS NULL OR broadcaster_id = ''` の異なる `movie_id` を列挙する
2. APIから配信者ID・screen ID・名前を取得する
3. 同じ `movie_id` の全セッションを1トランザクションで更新する
4. 補完済みの配信はスキップし、何度でも安全に再実行できる
5. APIエラーはmovie ID単位で記録し、残りを継続する
6. 最後に成功配信数・失敗配信数・更新セッション数を表示する

ダッシュボード起動時には自動補完しない。起動が外部APIの状態に左右されることと、閲覧操作が暗黙にDBを変更することを避ける。

未補完または取得失敗のデータは「配信者不明」として扱い、曜日別平均には含めない。これにより、既知の配信者同士が混在しない。

## 9. バリデーションとエラー表示

- 配信者一覧0件: セレクターを無効化し、配信者情報の補完または収集開始を案内する
- 選択配信者の配信0件: サイドバーと詳細領域を空状態にする
- 配信者一覧取得失敗: 既存表示を保持し、ヘッダーに取得エラーを表示する
- 配信一覧または分析取得失敗: 該当領域だけエラー表示し、別領域の更新は継続する
- `broadcaster_id` なしの分析API呼び出し: 400
- 不明な `broadcaster_id`: 404

## 10. テスト方針

### DB

- 新規セッションに配信者3項目が保存・取得される
- 2配信者、複数movie、複数sessionを作り、配信一覧が選択配信者だけになる
- 曜日別平均が選択配信者のスナップショットだけで計算される
- 配信者未設定のスナップショットは平均から除外される
- 最新セッションの配信者表示情報が一覧に使われる

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

1. API型で`BroadcasterID`を保持する
2. DB型と配信者別クエリを定義する
3. Monitorから配信者情報を保存する
4. 配信者情報補完コマンドを提供する
5. Dashboard APIで配信者IDを必須にする
6. 配信者セレクターとフロントエンド状態を管理する
7. DB・Monitor・API・UIをテストする

## 12. 完了条件

- 画面上で各保存データの配信者を識別できる
- 配信者変更直後と自動更新後の両方で、サイドバーに別配信者の配信が混ざらない
- 曜日別平均のSQLが必ず `broadcaster_id` で絞り込まれ、別配信者および配信者不明データを含まない
- 配信者情報の補完は中断後も安全に再実行できる
