package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAPIClient(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/users/twitcasting_jp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"user":{"id":"12345","screen_id":"twitcasting_jp","name":"TwitCasting Official"}}`))
	})

	mux.HandleFunc("/movies/189037369", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"movie": {
				"id": "189037369",
				"title": "Test Title",
				"subtitle": "Test Subtitle",
				"is_live": true,
				"is_recorded": false,
				"comment_count": 100,
				"current_view_count": 50,
				"max_view_count": 80,
				"total_view_count": 500,
				"duration": 1200,
				"created": 1700000000
			},
			"broadcaster": {
				"id": "12345",
				"screen_id": "twitcasting_jp",
				"name": "TwitCasting Official"
			}
		}`))
	})

	mux.HandleFunc("/users/offline_user/current_live", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":{"code":404,"message":"Not Found"}}`))
	})
	mux.HandleFunc("/users/offline_user", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"user":{"id":"offline_user","is_live":false,"last_movie_id":null}}`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewClient("dummy_id", "dummy_secret", 5*time.Second)
	client.SetBaseURL(server.URL)

	ctx := context.Background()

	// Test GetUser
	user, err := client.GetUser(ctx, "twitcasting_jp")
	if err != nil {
		t.Fatalf("GetUser failed: %v", err)
	}
	if user.ScreenID != "twitcasting_jp" {
		t.Errorf("expected twitcasting_jp, got %s", user.ScreenID)
	}

	// Test GetMovieInfo
	movie, err := client.GetMovieInfo(ctx, "189037369")
	if err != nil {
		t.Fatalf("GetMovieInfo failed: %v", err)
	}
	if movie.MovieID != "189037369" || movie.Title != "Test Title" || !movie.IsLive {
		t.Errorf("unexpected movie info: %+v", movie)
	}
	if movie.BroadcasterID != "12345" || movie.BroadcasterScreenID != "twitcasting_jp" {
		t.Errorf("unexpected broadcaster info: %+v", movie)
	}

	// Test GetCurrentLive on offline user
	live, err := client.GetCurrentLive(ctx, "offline_user")
	if err != nil {
		t.Fatalf("GetCurrentLive offline test failed: %v", err)
	}
	if live != nil {
		t.Errorf("expected nil for offline user, got %+v", live)
	}
}

func TestGetCurrentLiveFallback(t *testing.T) {
	liveUser := `{"user":{"id":"100","screen_id":"test_user","is_live":true,"last_movie_id":"movie_123"}}`
	liveMovie := `{"movie":{"id":"movie_123","is_live":true,"title":"Live","current_view_count":50},"broadcaster":{"id":"100","screen_id":"test_user","name":"Test User"}}`
	tests := []struct {
		name          string
		currentStatus int
		currentBody   string
		userStatus    int
		userBody      string
		movieStatus   int
		movieBody     string
		wantLive      bool
		wantError     bool
		wantStatus    int
		wantPaths     []string
	}{
		{name: "normal current live", currentStatus: 200, currentBody: liveMovie, wantLive: true, wantPaths: []string{"current"}},
		{name: "404 while live", userBody: liveUser, movieBody: liveMovie, wantLive: true, wantPaths: []string{"current", "user", "movie"}},
		{name: "offline with null movie", userBody: `{"user":{"id":"100","is_live":false,"last_movie_id":null}}`, wantPaths: []string{"current", "user"}},
		{name: "offline with old movie", userBody: `{"user":{"id":"100","is_live":false,"last_movie_id":"movie_123"}}`, wantPaths: []string{"current", "user"}},
		{name: "live with null movie", userBody: `{"user":{"id":"100","is_live":true,"last_movie_id":null}}`, wantError: true, wantPaths: []string{"current", "user"}},
		{name: "live with empty movie", userBody: `{"user":{"id":"100","is_live":true,"last_movie_id":""}}`, wantError: true, wantPaths: []string{"current", "user"}},
		{name: "live missing user id", userBody: `{"user":{"is_live":true,"last_movie_id":"movie_123"}}`, wantError: true, wantPaths: []string{"current", "user"}},
		{name: "movie ended during check", userBody: liveUser, movieBody: `{"movie":{"id":"movie_123","is_live":false},"broadcaster":{"id":"other"}}`, wantPaths: []string{"current", "user", "movie"}},
		{name: "movie disappeared", userBody: liveUser, movieStatus: 404, wantPaths: []string{"current", "user", "movie"}},
		{name: "wrong broadcaster", userBody: liveUser, movieBody: `{"movie":{"id":"movie_123","is_live":true},"broadcaster":{"id":"other"}}`, wantError: true, wantPaths: []string{"current", "user", "movie"}},
		{name: "missing broadcaster", userBody: liveUser, movieBody: `{"movie":{"id":"movie_123","is_live":true}}`, wantError: true, wantPaths: []string{"current", "user", "movie"}},
		{name: "wrong movie", userBody: liveUser, movieBody: `{"movie":{"id":"other","is_live":true},"broadcaster":{"id":"100"}}`, wantError: true, wantPaths: []string{"current", "user", "movie"}},
		{name: "missing movie id", userBody: liveUser, movieBody: `{"movie":{"is_live":true},"broadcaster":{"id":"100"}}`, wantError: true, wantPaths: []string{"current", "user", "movie"}},
		{name: "current unauthorized", currentStatus: 401, wantError: true, wantStatus: 401, wantPaths: []string{"current"}},
		{name: "current rate limited", currentStatus: 429, wantError: true, wantStatus: 429, wantPaths: []string{"current"}},
		{name: "user missing", userStatus: 404, wantError: true, wantStatus: 404, wantPaths: []string{"current", "user"}},
		{name: "user unauthorized", userStatus: 401, wantError: true, wantStatus: 401, wantPaths: []string{"current", "user"}},
		{name: "user rate limited", userStatus: 429, wantError: true, wantStatus: 429, wantPaths: []string{"current", "user"}},
		{name: "movie forbidden", userBody: liveUser, movieStatus: 403, wantError: true, wantStatus: 403, wantPaths: []string{"current", "user", "movie"}},
		{name: "movie server error", userBody: liveUser, movieStatus: 500, wantError: true, wantStatus: 500, wantPaths: []string{"current", "user", "movie"}},
		{name: "bad current json", currentStatus: 200, currentBody: `{`, wantError: true, wantPaths: []string{"current"}},
		{name: "bad user json", userBody: `{`, wantError: true, wantPaths: []string{"current", "user"}},
		{name: "bad movie json", userBody: liveUser, movieBody: `{`, wantError: true, wantPaths: []string{"current", "user", "movie"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var paths []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Basic aWQ6c2VjcmV0" || r.Header.Get("X-Api-Version") != "2.0" {
					t.Error("missing authentication or API version")
				}
				status, body := 0, ""
				switch r.URL.Path {
				case "/users/test_user/current_live":
					paths = append(paths, "current")
					status, body = tt.currentStatus, tt.currentBody
					if status == 0 {
						status = http.StatusNotFound
					}
				case "/users/test_user":
					paths = append(paths, "user")
					status, body = tt.userStatus, tt.userBody
				case "/movies/movie_123":
					paths = append(paths, "movie")
					status, body = tt.movieStatus, tt.movieBody
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					status = http.StatusNotFound
				}
				if status == 0 {
					status = http.StatusOK
				}
				w.WriteHeader(status)
				w.Write([]byte(body))
			}))
			defer server.Close()
			client := NewClient("id", "secret", time.Second)
			client.SetBaseURL(server.URL)
			movie, err := client.GetCurrentLive(context.Background(), "test_user")
			if (err != nil) != tt.wantError {
				t.Fatalf("error = %v, wantError = %t", err, tt.wantError)
			}
			if tt.wantStatus != 0 {
				var apiErr *APIError
				if !errors.As(err, &apiErr) || apiErr.HTTPStatus != tt.wantStatus {
					t.Fatalf("expected HTTP %d APIError, got %v", tt.wantStatus, err)
				}
			}
			if tt.wantLive {
				if movie == nil || !movie.IsLive || movie.MovieID != "movie_123" || movie.BroadcasterID != "100" || movie.CurrentViewCount != 50 {
					t.Fatalf("unexpected live snapshot: %+v", movie)
				}
			} else if movie != nil {
				t.Fatalf("expected no live snapshot, got %+v", movie)
			}
			if !reflect.DeepEqual(paths, tt.wantPaths) {
				t.Fatalf("requests = %v, want %v", paths, tt.wantPaths)
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGetCurrentLiveFallbackCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := NewClient("id", "secret", time.Second)
	var paths []string
	client.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/users/test_user/current_live" {
			return &http.Response{StatusCode: http.StatusNotFound, Body: http.NoBody, Header: make(http.Header)}, nil
		}
		cancel()
		return nil, r.Context().Err()
	})
	movie, err := client.GetCurrentLive(ctx, "test_user")
	if movie != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation without snapshot, got %+v, %v", movie, err)
	}
	if !reflect.DeepEqual(paths, []string{"/users/test_user/current_live", "/users/test_user"}) {
		t.Fatalf("unexpected requests: %v", paths)
	}
}

type cancelBody struct{ cancel context.CancelFunc }

func (b cancelBody) Read([]byte) (int, error) {
	b.cancel()
	return 0, context.Canceled
}

func (b cancelBody) Close() error { return nil }

type errorBody struct{ err error }

func (b errorBody) Read([]byte) (int, error) { return 0, b.err }
func (b errorBody) Close() error             { return nil }

func TestGetCurrentLiveFallbackMovie404ReadError(t *testing.T) {
	for _, readErr := range []error{io.ErrUnexpectedEOF, context.DeadlineExceeded} {
		t.Run(readErr.Error(), func(t *testing.T) {
			client := NewClient("id", "secret", time.Second)
			client.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				resp := &http.Response{StatusCode: http.StatusNotFound, Body: http.NoBody, Header: make(http.Header)}
				switch r.URL.Path {
				case "/users/test_user/current_live":
				case "/users/test_user":
					resp.StatusCode = http.StatusOK
					resp.Body = io.NopCloser(strings.NewReader(`{"user":{"id":"100","is_live":true,"last_movie_id":"movie_123"}}`))
				case "/movies/movie_123":
					resp.Body = errorBody{err: readErr}
				default:
					t.Fatalf("unexpected path %s", r.URL.Path)
				}
				return resp, nil
			})
			movie, err := client.GetCurrentLive(context.Background(), "test_user")
			if movie != nil || !errors.Is(err, readErr) {
				t.Fatalf("404 read error must not become offline: movie=%+v, err=%v", movie, err)
			}
		})
	}
}

func TestGetCurrentLiveFallbackMovie404Cancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := NewClient("id", "secret", time.Second)
	client.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		resp := &http.Response{StatusCode: http.StatusNotFound, Body: http.NoBody, Header: make(http.Header)}
		switch r.URL.Path {
		case "/users/test_user/current_live":
		case "/users/test_user":
			resp.StatusCode = http.StatusOK
			resp.Body = io.NopCloser(strings.NewReader(`{"user":{"id":"100","is_live":true,"last_movie_id":"movie_123"}}`))
		case "/movies/movie_123":
			resp.Body = cancelBody{cancel: cancel}
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return resp, nil
	})
	movie, err := client.GetCurrentLive(ctx, "test_user")
	if movie != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("404 body cancellation must not become offline: movie=%+v, err=%v", movie, err)
	}
}
