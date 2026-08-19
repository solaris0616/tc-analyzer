# 設計書 02 — TwitCasting API クライアント (`internal/api`)

## 1. 概要

Python 版 `api.py` に相当する TwitCasting API v2 クライアント。
Basic 認証（ClientID:ClientSecret の Base64 エンコード）を使用する。

---

## 2. API エンドポイント

| エンドポイント | メソッド | 説明 |
|---|---|---|
| `GET /users/{user_id}` | `GetUser` | ユーザー情報の取得（存在確認用） |
| `GET /movies/{movie_id}` | `GetMovieInfo` | 配信情報取得（ライブ・録画共通） |
| `GET /users/{user_id}/current_live` | `GetCurrentLive` | ユーザーの現在のライブ情報取得 |
| `GET /verify_credentials` | `VerifyCredentials` | 認証情報の検証 |

- ベース URL: `https://apiv2.twitcasting.tv`
- API バージョンヘッダー: `X-Api-Version: 2.0`
- レートリミット: 60 リクエスト / 60 秒

---

## 3. データ型定義

### MovieSnapshot

Python 版 `MovieSnapshot` dataclass に相当する。

```go
package api

// MovieSnapshot はある時点での配信情報スナップショット
type MovieSnapshot struct {
    MovieID              string  `json:"id"`
    Title                string  `json:"title"`
    Subtitle             string  `json:"subtitle"`      // 空文字 = 未設定
    IsLive               bool    `json:"is_live"`
    IsRecorded           bool    `json:"is_recorded"`
    CommentCount         int     `json:"comment_count"`
    CurrentViewCount     int     `json:"current_view_count"`
    MaxViewCount         int     `json:"max_view_count"`
    TotalViewCount       int     `json:"total_view_count"`
    Duration             int     `json:"duration"`
    CreatedAt            int64   `json:"created"`        // UNIX timestamp
    BroadcasterScreenID  string  // broadcaster.screen_id
    BroadcasterName      string  // broadcaster.name
}
```

### VerifyCredentialsResponse

```go
// VerifyCredentialsResponse は /verify_credentials のレスポンス
type VerifyCredentialsResponse struct {
    App  AppInfo  `json:"app"`
    User UserInfo `json:"user"`
}

type AppInfo struct {
    ClientID string `json:"client_id"`
    Name     string `json:"name"`
}

type UserInfo struct {
    ID       string `json:"id"`
    ScreenID string `json:"screen_id"`
    Name     string `json:"name"`
}
```

---

## 4. エラー型

```go
// APIError は TwitCasting API のエラーレスポンスを表す
type APIError struct {
    Message    string
    Code       int    // API エラーコード (0 = 不明)
    HTTPStatus int    // HTTP ステータスコード
}

func (e *APIError) Error() string {
    return fmt.Sprintf("HTTP %d: %s (code=%d)", e.HTTPStatus, e.Message, e.Code)
}
```

---

## 5. クライアント型

```go
// Client は TwitCasting API v2 クライアント
type Client struct {
    httpClient  *http.Client
    baseURL     string
    headers     map[string]string
    limiter     *rate.Limiter // golang.org/x/time/rate によるレートリミッター
}

// NewClient は新しい API クライアントを作成する
// clientID と clientSecret を Base64 エンコードして Basic 認証ヘッダーを設定する
// また、TwitCasting APIのレートリミット（60回/60秒）を制御するリミッターを初期化する
func NewClient(clientID, clientSecret string, timeout time.Duration) *Client

// GetUser は GET /users/{user_id} を呼び出し、ユーザー情報を返す
// ユーザーが存在しない場合は 404 エラー（APIError）が返る
func (c *Client) GetUser(ctx context.Context, userID string) (*UserInfo, error)

// GetMovieInfo は GET /movies/{movie_id} を呼び出す
func (c *Client) GetMovieInfo(ctx context.Context, movieID string) (*MovieSnapshot, error)

// GetCurrentLive は GET /users/{user_id}/current_live を呼び出す
// 配信中でない場合は (nil, nil) を返す（404 は正常系として扱う）
func (c *Client) GetCurrentLive(ctx context.Context, userID string) (*MovieSnapshot, error)

// VerifyCredentials は GET /verify_credentials を呼び出す
func (c *Client) VerifyCredentials(ctx context.Context) (*VerifyCredentialsResponse, error)
```

---

## 6. 実装詳細

### リミッターの初期化 (NewClient)

```go
func NewClient(clientID, clientSecret string, timeout time.Duration) *Client {
    // 60回 / 60秒 の制限に基づき、1秒あたり最大1トークン、バースト最大60で制限
    limiter := rate.NewLimiter(rate.Every(1*time.Second), 60)
    
    // ヘッダー生成等
    ...
    return &Client{
        httpClient: &http.Client{Timeout: timeout},
        baseURL:    API_BASE,
        headers:    headers,
        limiter:    limiter,
    }
}
```

### 認証ヘッダー生成

```go
func newAuthHeader(clientID, clientSecret string) string {
    raw := clientID + ":" + clientSecret
    encoded := base64.StdEncoding.EncodeToString([]byte(raw))
    return "Basic " + encoded
}
```

### レート制限付きリクエスト送信

```go
func (c *Client) do(ctx context.Context, method, path string) (*http.Response, error) {
    // リクエスト送信前にレートリミットを待機
    if err := c.limiter.Wait(ctx); err != nil {
        return nil, err
    }

    req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
    if err != nil {
        return nil, err
    }
    for k, v := range c.headers {
        req.Header.Set(k, v)
    }
    return c.httpClient.Do(req)
}
```

### エラーチェック

```go
func checkResponse(resp *http.Response) error {
    if resp.StatusCode == http.StatusOK {
        return nil
    }
    defer resp.Body.Close()
    body, _ := io.ReadAll(resp.Body)

    var errBody struct {
        Error struct {
            Code    int    `json:"code"`
            Message string `json:"message"`
        } `json:"error"`
    }
    if err := json.Unmarshal(body, &errBody); err == nil && errBody.Error.Message != "" {
        return &APIError{
            Message:    errBody.Error.Message,
            Code:       errBody.Error.Code,
            HTTPStatus: resp.StatusCode,
        }
    }
    return &APIError{
        Message:    string(body),
        HTTPStatus: resp.StatusCode,
    }
}
```

### GetCurrentLive の 404 ハンドリング

```go
func (c *Client) GetCurrentLive(ctx context.Context, userID string) (*MovieSnapshot, error) {
    resp, err := c.do(ctx, "GET", "/users/"+userID+"/current_live")
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    // 404 は「配信中でない」を意味する正常系
    if resp.StatusCode == http.StatusNotFound {
        return nil, nil
    }
    if err := checkResponse(resp); err != nil {
        return nil, err
    }
    // JSON デコード → MovieSnapshot 生成
    ...
}
```

---

## 7. API レスポンス JSON 構造

### GET /movies/{movie_id}

```json
{
  "movie": {
    "id": "189037369",
    "title": "配信タイトル",
    "subtitle": "サブタイトル",
    "is_live": true,
    "is_recorded": false,
    "comment_count": 1234,
    "current_view_count": 567,
    "max_view_count": 1000,
    "total_view_count": 9999,
    "duration": 3600,
    "created": 1700000000
  },
  "broadcaster": {
    "id": "1234567",
    "screen_id": "twitcasting_jp",
    "name": "TwitCasting 公式"
  }
}
```

### GET /users/{user_id}/current_live

同じ構造。配信中でない場合は HTTP 404 が返る。

---

## 8. Python 版との差分

| 項目 | Python 版 | Go 版 |
|---|---|---|
| 非同期 | `async/await` | 同期 (`context.Context` でキャンセル対応) |
| HTTP クライアント | `httpx.AsyncClient` | `net/http.Client` |
| 接続管理 | `async with` コンテキストマネージャ | `Client` 構造体を使い回す（`http.Client` は再利用可能） |
| Optional | `Optional[MovieSnapshot]` | `*MovieSnapshot` (nil pointer) |
| subtitle | `Optional[str]` | `string` (空文字 = 未設定) |
