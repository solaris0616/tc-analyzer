package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/time/rate"
)

const APIBaseURL = "https://apiv2.twitcasting.tv"

// MovieSnapshot represents live stream metadata at a specific moment.
type MovieSnapshot struct {
	MovieID             string `json:"id"`
	Title               string `json:"title"`
	Subtitle            string `json:"subtitle"`
	IsLive              bool   `json:"is_live"`
	IsRecorded          bool   `json:"is_recorded"`
	CommentCount        int    `json:"comment_count"`
	CurrentViewCount    int    `json:"current_view_count"`
	MaxViewCount        int    `json:"max_view_count"`
	TotalViewCount      int    `json:"total_view_count"`
	Duration            int    `json:"duration"`
	CreatedAt           int64  `json:"created"`
	BroadcasterScreenID string `json:"broadcaster_screen_id"`
	BroadcasterName     string `json:"broadcaster_name"`
}

// UserInfo represents TwitCasting user details.
type UserInfo struct {
	ID       string `json:"id"`
	ScreenID string `json:"screen_id"`
	Name     string `json:"name"`
}

// Comment represents a single comment on a live stream.
type Comment struct {
	ID       string   `json:"id"`
	Message  string   `json:"message"`
	FromUser UserInfo `json:"from_user"`
	Created  int64    `json:"created"`
}

// GetCommentsResponse wraps the TwitCasting API comments response.
type GetCommentsResponse struct {
	MovieID  string    `json:"movie_id"`
	AllCount int       `json:"all_count"`
	Comments []Comment `json:"comments"`
}

// AppInfo represents TwitCasting application details.
type AppInfo struct {
	ClientID string `json:"client_id"`
	Name     string `json:"name"`
}

// VerifyCredentialsResponse represents the response from /verify_credentials.
type VerifyCredentialsResponse struct {
	App  AppInfo  `json:"app"`
	User UserInfo `json:"user"`
}

// APIError represents an error response from the TwitCasting API.
type APIError struct {
	Message    string
	Code       int
	HTTPStatus int
}

func (e *APIError) Error() string {
	return fmt.Sprintf("HTTP %d: %s (code=%d)", e.HTTPStatus, e.Message, e.Code)
}

// Internal API JSON response wrapper
type movieResponseJSON struct {
	Movie struct {
		ID               string `json:"id"`
		Title            string `json:"title"`
		Subtitle         string `json:"subtitle"`
		IsLive           bool   `json:"is_live"`
		IsRecorded       bool   `json:"is_recorded"`
		CommentCount     int    `json:"comment_count"`
		CurrentViewCount int    `json:"current_view_count"`
		MaxViewCount     int    `json:"max_view_count"`
		TotalViewCount   int    `json:"total_view_count"`
		Duration         int    `json:"duration"`
		Created          int64  `json:"created"`
	} `json:"movie"`
	Broadcaster struct {
		ID       string `json:"id"`
		ScreenID string `json:"screen_id"`
		Name     string `json:"name"`
	} `json:"broadcaster"`
}

type userResponseJSON struct {
	User UserInfo `json:"user"`
}

// Client is a TwitCasting API v2 client.
type Client struct {
	httpClient *http.Client
	baseURL    string
	headers    map[string]string
	limiter    *rate.Limiter
}

// NewClient creates and initializes a new TwitCasting API client.
func NewClient(clientID, clientSecret string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	// TwitCasting API limit: 60 requests / 60 seconds
	limiter := rate.NewLimiter(rate.Every(1*time.Second), 60)

	auth := base64.StdEncoding.EncodeToString([]byte(clientID + ":" + clientSecret))
	headers := map[string]string{
		"Authorization":  "Basic " + auth,
		"X-Api-Version":  "2.0",
		"Accept":         "application/json",
	}

	return &Client{
		httpClient: &http.Client{Timeout: timeout},
		baseURL:    APIBaseURL,
		headers:    headers,
		limiter:    limiter,
	}
}

// SetBaseURL overrides the default API base URL (useful for testing).
func (c *Client) SetBaseURL(url string) {
	c.baseURL = url
}

func (c *Client) do(ctx context.Context, method, path string) (*http.Response, error) {
	if c.limiter != nil {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, err
		}
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

// GetUser calls GET /users/{user_id} and returns user info.
func (c *Client) GetUser(ctx context.Context, userID string) (*UserInfo, error) {
	resp, err := c.do(ctx, http.MethodGet, "/users/"+userID)
	if err != nil {
		return nil, err
	}
	if err := checkResponse(resp); err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var data userResponseJSON
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to decode user response: %w", err)
	}

	return &data.User, nil
}

// GetMovieInfo calls GET /movies/{movie_id} and returns details.
func (c *Client) GetMovieInfo(ctx context.Context, movieID string) (*MovieSnapshot, error) {
	resp, err := c.do(ctx, http.MethodGet, "/movies/"+movieID)
	if err != nil {
		return nil, err
	}
	if err := checkResponse(resp); err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var data movieResponseJSON
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to decode movie response: %w", err)
	}

	return parseMovieSnapshot(&data), nil
}

// GetCurrentLive calls GET /users/{user_id}/current_live. Returns (nil, nil) if not currently live (404).
func (c *Client) GetCurrentLive(ctx context.Context, userID string) (*MovieSnapshot, error) {
	resp, err := c.do(ctx, http.MethodGet, "/users/"+userID+"/current_live")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, nil
	}
	if err := checkResponse(resp); err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var data movieResponseJSON
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to decode current live response: %w", err)
	}

	return parseMovieSnapshot(&data), nil
}

// VerifyCredentials calls GET /verify_credentials.
func (c *Client) VerifyCredentials(ctx context.Context) (*VerifyCredentialsResponse, error) {
	resp, err := c.do(ctx, http.MethodGet, "/verify_credentials")
	if err != nil {
		return nil, err
	}
	if err := checkResponse(resp); err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var res VerifyCredentialsResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("failed to decode verify credentials response: %w", err)
	}

	return &res, nil
}

func parseMovieSnapshot(data *movieResponseJSON) *MovieSnapshot {
	return &MovieSnapshot{
		MovieID:             data.Movie.ID,
		Title:               data.Movie.Title,
		Subtitle:            data.Movie.Subtitle,
		IsLive:              data.Movie.IsLive,
		IsRecorded:          data.Movie.IsRecorded,
		CommentCount:        data.Movie.CommentCount,
		CurrentViewCount:    data.Movie.CurrentViewCount,
		MaxViewCount:        data.Movie.MaxViewCount,
		TotalViewCount:      data.Movie.TotalViewCount,
		Duration:            data.Movie.Duration,
		CreatedAt:           data.Movie.Created,
		BroadcasterScreenID: data.Broadcaster.ScreenID,
		BroadcasterName:     data.Broadcaster.Name,
	}
}

// GetComments calls GET /movies/{movie_id}/comments.
// sliceID: fetch comments newer than this comment ID (empty = latest).
// limit: max number of comments to return (API max: 50).
func (c *Client) GetComments(ctx context.Context, movieID string, sliceID string, limit int) (*GetCommentsResponse, error) {
	if limit <= 0 || limit > 50 {
		limit = 50
	}
	path := fmt.Sprintf("/movies/%s/comments?limit=%d", movieID, limit)
	if sliceID != "" {
		path += "&slice_id=" + sliceID
	}

	resp, err := c.do(ctx, http.MethodGet, path)
	if err != nil {
		return nil, err
	}
	if err := checkResponse(resp); err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var raw struct {
		MovieID  string `json:"movie_id"`
		AllCount int    `json:"all_count"`
		Comments []struct {
			ID      string `json:"id"`
			Message string `json:"message"`
			Author  struct {
				ID       string `json:"id"`
				ScreenID string `json:"screen_id"`
				Name     string `json:"name"`
			} `json:"from_user"`
			Created int64 `json:"created"`
		} `json:"comments"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("failed to decode comments response: %w", err)
	}

	out := &GetCommentsResponse{
		MovieID:  raw.MovieID,
		AllCount: raw.AllCount,
	}
	for _, c := range raw.Comments {
		out.Comments = append(out.Comments, Comment{
			ID:      c.ID,
			Message: c.Message,
			FromUser: UserInfo{
				ID:       c.Author.ID,
				ScreenID: c.Author.ScreenID,
				Name:     c.Author.Name,
			},
			Created: c.Created,
		})
	}
	return out, nil
}
