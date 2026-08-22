package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"tc-analyzer/internal/api"
	"tc-analyzer/internal/db"
)

func TestDashboardServer(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.New(filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	// Insert test data
	sessionID, err := database.CreateSessionWithBroadcaster("movie_test", "Dashboard Test", db.Broadcaster{ID: "user_100", ScreenID: "tester", Name: "Tester"})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	snap := &api.MovieSnapshot{
		MovieID:          "movie_test",
		Title:            "Dashboard Test Title",
		IsLive:           true,
		CommentCount:     50,
		CurrentViewCount: 100,
		MaxViewCount:     150,
		TotalViewCount:   500,
		Duration:         600,
	}
	if _, err := database.AddSnapshot(sessionID, snap); err != nil {
		t.Fatalf("failed to add snapshot: %v", err)
	}

	server := NewServer(database, "127.0.0.1", 0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = server.Start(ctx)
	}()

	// Test endpoints with httptest against handlers directly
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/broadcasters", server.handleGetBroadcasters)
	mux.HandleFunc("GET /api/movies", server.handleGetMovies)
	mux.HandleFunc("GET /api/movies/{movie_id}", server.handleGetMovieDetail)
	mux.HandleFunc("GET /api/analysis", server.handleGetAnalysis)

	reqBroadcasters := httptest.NewRequest("GET", "/api/broadcasters", nil)
	recBroadcasters := httptest.NewRecorder()
	mux.ServeHTTP(recBroadcasters, reqBroadcasters)
	if recBroadcasters.Code != http.StatusOK {
		t.Fatalf("expected status 200 for /api/broadcasters, got %d", recBroadcasters.Code)
	}
	var broadcasters []*db.BroadcasterListRow
	if err := json.Unmarshal(recBroadcasters.Body.Bytes(), &broadcasters); err != nil {
		t.Fatalf("failed to unmarshal broadcasters: %v", err)
	}
	if len(broadcasters) != 1 || broadcasters[0].ID != "user_100" {
		t.Fatalf("unexpected broadcasters output: %+v", broadcasters)
	}

	// Test GET /api/movies
	req := httptest.NewRequest("GET", "/api/movies?broadcaster_id=user_100", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 for /api/movies, got %d", rec.Code)
	}

	var movies []*db.MovieListRow
	if err := json.Unmarshal(rec.Body.Bytes(), &movies); err != nil {
		t.Fatalf("failed to unmarshal movies: %v", err)
	}
	if len(movies) != 1 || movies[0].MovieID != "movie_test" {
		t.Errorf("unexpected movies output: %+v", movies)
	}
	reqMoviesMissingFilter := httptest.NewRequest("GET", "/api/movies", nil)
	recMoviesMissingFilter := httptest.NewRecorder()
	mux.ServeHTTP(recMoviesMissingFilter, reqMoviesMissingFilter)
	if recMoviesMissingFilter.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for movies without broadcaster filter, got %d", recMoviesMissingFilter.Code)
	}

	reqAnalysis := httptest.NewRequest("GET", "/api/analysis?broadcaster_id=user_100", nil)
	recAnalysis := httptest.NewRecorder()
	mux.ServeHTTP(recAnalysis, reqAnalysis)
	if recAnalysis.Code != http.StatusOK {
		t.Fatalf("expected status 200 for filtered analysis, got %d", recAnalysis.Code)
	}

	reqMissingFilter := httptest.NewRequest("GET", "/api/analysis", nil)
	recMissingFilter := httptest.NewRecorder()
	mux.ServeHTTP(recMissingFilter, reqMissingFilter)
	if recMissingFilter.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 without broadcaster filter, got %d", recMissingFilter.Code)
	}

	// Test GET /api/movies/movie_test
	reqDetail := httptest.NewRequest("GET", "/api/movies/movie_test", nil)
	recDetail := httptest.NewRecorder()
	mux.ServeHTTP(recDetail, reqDetail)

	if recDetail.Code != http.StatusOK {
		t.Fatalf("expected status 200 for /api/movies/movie_test, got %d", recDetail.Code)
	}

	var detail MovieDetailResponse
	if err := json.Unmarshal(recDetail.Body.Bytes(), &detail); err != nil {
		t.Fatalf("failed to unmarshal movie detail: %v", err)
	}
	if detail.Movie.MovieID != "movie_test" || len(detail.Snapshots) != 1 {
		t.Errorf("unexpected movie detail output: %+v", detail)
	}

	// Wait briefly for server startup cancellation
	cancel()
	time.Sleep(50 * time.Millisecond)
}
