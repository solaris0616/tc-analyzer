package monitor

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"tc-analyzer/internal/api"
	"tc-analyzer/internal/db"
)

func TestMonitorMovie(t *testing.T) {
	mux := http.NewServeMux()

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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"movie": {
				"id": "movie_123",
				"title": "Live Title",
				"is_live": false,
				"comment_count": 12,
				"current_view_count": 0,
				"max_view_count": 60,
				"total_view_count": 210,
				"duration": 120,
				"created": 1700000000
			},
			"broadcaster": {"id":"100","screen_id":"test_user","name":"Test User"}
		}`))
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
	if len(snaps) == 0 {
		t.Fatalf("expected snapshots in database")
	}
}

func TestMonitorMovieCancelsCommentPollingOnNaturalEnd(t *testing.T) {
	commentStarted := make(chan struct{})
	commentCanceled := make(chan struct{})
	moviePolls := 0

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
		close(commentStarted)
		<-r.Context().Done()
		close(commentCanceled)
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
		Duration:        time.Second,
		CommentInterval: 5 * time.Millisecond,
		Writer:          &buf,
	})
	if err != nil {
		t.Fatalf("MonitorMovie failed: %v", err)
	}

	select {
	case <-commentStarted:
	default:
		t.Fatal("comment polling did not start")
	}
	select {
	case <-commentCanceled:
	default:
		t.Fatal("MonitorMovie returned before comment polling stopped")
	}
}
