package monitor

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"tc-analyzer/internal/api"
	"tc-analyzer/internal/db"
)

func TestMonitorMovie(t *testing.T) {
	mux := http.NewServeMux()
	moviePolls := 0

	mux.HandleFunc("/users/test_user", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"user":{"id":"100","screen_id":"test_user","name":"Test User"}}`))
	})

	mux.HandleFunc("/users/test_user/current_live", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"movie": {
				"id": "movie_123",
				"title": "Live Title",
				"is_live": true,
				"comment_count": 10,
				"current_view_count": 50,
				"max_view_count": 60,
				"total_view_count": 200,
				"duration": 100,
				"created": 1700000000
			},
			"broadcaster": {"id":"100","screen_id":"test_user","name":"Test User"}
		}`))
	})

	mux.HandleFunc("/movies/movie_123", func(w http.ResponseWriter, r *http.Request) {
		moviePolls++
		isLive := moviePolls == 1
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{
			"movie": {
				"id": "movie_123",
				"title": "Live Title",
				"is_live": %t,
				"comment_count": 12,
				"current_view_count": 0,
				"max_view_count": 60,
				"total_view_count": 210,
				"duration": 120,
				"created": 1700000000
			},
			"broadcaster": {"id":"100","screen_id":"test_user","name":"Test User"}
		}`, isLive)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := api.NewClient("id", "secret", 5*time.Second)
	client.SetBaseURL(server.URL)

	tempDir := t.TempDir()
	database, err := db.New(filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	var buf bytes.Buffer
	snapshotCount := 0

	opts := MonitorOptions{
		Interval: 100 * time.Millisecond,
		Duration: 1 * time.Second,
		Label:    "Test Label",
		Writer:   &buf,
		OnSnapshot: func(snap *api.MovieSnapshot, sessionID int64) {
			snapshotCount++
		},
	}

	ctx := context.Background()
	sessionID, err := MonitorMovie(ctx, client, database, "test_user", opts)
	if err != nil {
		t.Fatalf("MonitorMovie failed: %v", err)
	}
	if sessionID <= 0 {
		t.Fatalf("invalid sessionID: %d", sessionID)
	}
	if snapshotCount == 0 {
		t.Fatalf("expected at least 1 snapshot callback, got 0")
	}

	snaps, err := database.GetSnapshots(sessionID)
	if err != nil {
		t.Fatalf("failed to get snapshots: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("expected only the live snapshot in database, got %d", len(snaps))
	}
	session, err := database.GetSession(sessionID)
	if err != nil {
		t.Fatalf("failed to get session: %v", err)
	}
	if session.BroadcasterID != "100" || session.BroadcasterScreenID != "test_user" || session.BroadcasterName != "Test User" {
		t.Fatalf("broadcaster metadata was not saved: %+v", session)
	}
}

func TestMonitorMovieCurrentLive404(t *testing.T) {
	for _, wait := range []bool{false, true} {
		t.Run(fmt.Sprintf("wait=%t", wait), func(t *testing.T) {
			var userCalls, movieCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/users/test_user":
					call := userCalls.Add(1)
					isLive := !wait || call >= 3
					fmt.Fprintf(w, `{"user":{"id":"100","screen_id":"test_user","name":"Test User","is_live":%t,"last_movie_id":"movie_123"}}`, isLive)
				case "/users/test_user/current_live":
					w.WriteHeader(http.StatusNotFound)
					fmt.Fprint(w, `{"error":{"code":404,"message":"Not Found"}}`)
				case "/movies/movie_123":
					isLive := movieCalls.Add(1) <= 2
					fmt.Fprintf(w, `{"movie":{"id":"movie_123","title":"Live","is_live":%t,"comment_count":12,"current_view_count":50},"broadcaster":{"id":"100","screen_id":"test_user","name":"Test User"}}`, isLive)
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			client := api.NewClient("id", "secret", time.Second)
			client.SetBaseURL(server.URL)
			database, err := db.New(filepath.Join(t.TempDir(), "test.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			var out bytes.Buffer
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			sessionID, err := MonitorMovie(ctx, client, database, "test_user", MonitorOptions{
				Interval: time.Millisecond, WaitInterval: time.Millisecond,
				WaitOnConfig: wait, Writer: &out,
			})
			if err != nil || sessionID <= 0 {
				t.Fatalf("monitor failed to start: session=%d err=%v", sessionID, err)
			}
			snaps, err := database.GetSnapshots(sessionID)
			if err != nil || len(snaps) != 1 || snaps[0].CurrentViewCount != 50 {
				t.Fatalf("expected one live snapshot: %+v, err=%v", snaps, err)
			}
			if movieCalls.Load() != 3 || !strings.Contains(out.String(), "自動終了") {
				t.Fatalf("monitor did not end naturally: movieCalls=%d output=%s", movieCalls.Load(), out.String())
			}
		})
	}
}

func TestMonitorMovieCancelsCommentPollingOnNaturalEnd(t *testing.T) {
	moviePolls := 0
	var commentPolls atomic.Int32

	mux := http.NewServeMux()
	mux.HandleFunc("/users/test_user", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"user":{"id":"100","screen_id":"test_user","name":"Test User"}}`))
	})
	mux.HandleFunc("/users/test_user/current_live", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"movie":{"id":"movie_123","title":"Live","is_live":true},"broadcaster":{"screen_id":"test_user","name":"Test User"}}`))
	})
	mux.HandleFunc("/movies/movie_123", func(w http.ResponseWriter, r *http.Request) {
		moviePolls++
		isLive := moviePolls == 1
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"movie":{"id":"movie_123","title":"Live","is_live":%t},"broadcaster":{"screen_id":"test_user","name":"Test User"}}`, isLive)
	})
	mux.HandleFunc("/movies/movie_123/comments", func(w http.ResponseWriter, r *http.Request) {
		commentPolls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"movie_id":"movie_123","all_count":0,"comments":[]}`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := api.NewClient("id", "secret", time.Second)
	client.SetBaseURL(server.URL)
	database, err := db.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	var buf bytes.Buffer
	_, err = MonitorMovie(context.Background(), client, database, "test_user", MonitorOptions{
		Interval:        50 * time.Millisecond,
		CommentInterval: 5 * time.Millisecond,
		Writer:          &buf,
	})
	if err != nil {
		t.Fatalf("MonitorMovie failed: %v", err)
	}

	pollsAtReturn := commentPolls.Load()
	if pollsAtReturn == 0 {
		t.Fatal("comment polling did not start")
	}
	time.Sleep(30 * time.Millisecond)
	if got := commentPolls.Load(); got != pollsAtReturn {
		t.Fatalf("comment polling continued after MonitorMovie returned: before=%d after=%d", pollsAtReturn, got)
	}
}

func TestMonitorMovieStopsWhenOfflineBroadcasterDiffers(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/users/test_user", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"user":{"id":"100","screen_id":"test_user","name":"Test User"}}`))
	})
	mux.HandleFunc("/users/test_user/current_live", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"movie":{"id":"movie_123","is_live":true},"broadcaster":{"id":"100","screen_id":"test_user","name":"Test User"}}`))
	})
	mux.HandleFunc("/movies/movie_123", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"movie":{"id":"movie_123","is_live":false},"broadcaster":{"id":"999","screen_id":"other","name":"Other"}}`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := api.NewClient("id", "secret", time.Second)
	client.SetBaseURL(server.URL)
	database, err := db.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	var buf bytes.Buffer
	sessionID, err := MonitorMovie(context.Background(), client, database, "test_user", MonitorOptions{
		Interval: 10 * time.Millisecond,
		Duration: 200 * time.Millisecond,
		Writer:   &buf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "オフライン") {
		t.Fatalf("monitor did not stop on the offline response: %q", buf.String())
	}
	snapshots, err := database.GetSnapshots(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 0 {
		t.Fatalf("offline response was stored: %+v", snapshots)
	}
}
