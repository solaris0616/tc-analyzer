package db

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"tc-analyzer/internal/api"
)

func TestDBOperations(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	database, err := New(dbPath)
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer database.Close()

	// Test CreateSession
	sessionID, err := database.CreateSessionWithBroadcaster("movie_100", 10, "Test Session", Broadcaster{ID: "user_1", ScreenID: "streamer", Name: "Streamer"})
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if sessionID <= 0 {
		t.Fatalf("invalid session ID: %d", sessionID)
	}

	// Test GetSession
	sess, err := database.GetSession(sessionID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if sess.MovieID != "movie_100" || sess.Label != "Test Session" || sess.IntervalSec != 10 {
		t.Errorf("unexpected session data: %+v", sess)
	}

	// Test AddSnapshot
	snap1 := &api.MovieSnapshot{
		MovieID:          "movie_100",
		Title:            "Test Movie Title",
		IsLive:           true,
		CommentCount:     10,
		CurrentViewCount: 20,
		MaxViewCount:     25,
		TotalViewCount:   100,
		Duration:         300,
	}

	snapID1, err := database.AddSnapshot(sessionID, snap1, 0, 10)
	if err != nil {
		t.Fatalf("AddSnapshot 1 failed: %v", err)
	}
	if snapID1 <= 0 {
		t.Fatalf("invalid snapshot ID: %d", snapID1)
	}

	snap2 := &api.MovieSnapshot{
		MovieID:          "movie_100",
		Title:            "Test Movie Title",
		IsLive:           true,
		CommentCount:     15,
		CurrentViewCount: 30,
		MaxViewCount:     35,
		TotalViewCount:   150,
		Duration:         310,
	}

	_, err = database.AddSnapshot(sessionID, snap2, 10, 5)
	if err != nil {
		t.Fatalf("AddSnapshot 2 failed: %v", err)
	}

	// Test GetSnapshots
	snaps, err := database.GetSnapshots(sessionID)
	if err != nil {
		t.Fatalf("GetSnapshots failed: %v", err)
	}
	if len(snaps) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(snaps))
	}
	if snaps[0].CurrentViewCount != 20 || snaps[1].CurrentViewCount != 30 {
		t.Errorf("unexpected snapshot viewer counts: %d, %d", snaps[0].CurrentViewCount, snaps[1].CurrentViewCount)
	}

	// Test GetSessionSummary
	summary, err := database.GetSessionSummary(sessionID)
	if err != nil {
		t.Fatalf("GetSessionSummary failed: %v", err)
	}
	if summary.TotalRecords != 2 {
		t.Errorf("expected 2 total records, got %d", summary.TotalRecords)
	}
	if summary.PeakViewers != 30 {
		t.Errorf("expected 30 peak viewers, got %d", summary.PeakViewers)
	}
	if summary.MinViewers != 20 {
		t.Errorf("expected 20 min viewers, got %d", summary.MinViewers)
	}
	if summary.AvgViewers != 25.0 {
		t.Errorf("expected 25.0 avg viewers, got %f", summary.AvgViewers)
	}
	if summary.TotalCommentsObserved != 15 {
		t.Errorf("expected 15 total comments observed, got %d", summary.TotalCommentsObserved)
	}

	// Test ListMovies
	movies, err := database.ListMovies()
	if err != nil {
		t.Fatalf("ListMovies failed: %v", err)
	}
	if len(movies) != 1 || movies[0].MovieID != "movie_100" {
		t.Errorf("unexpected movies list: %+v", movies)
	}

	// Test AnalysisData
	analysis, err := database.GetAnalysisData("user_1")
	if err != nil {
		t.Fatalf("GetAnalysisData failed: %v", err)
	}
	if len(analysis) == 0 {
		t.Errorf("expected analysis data, got empty")
	}
}

func TestBroadcasterFiltering(t *testing.T) {
	database, err := New(filepath.Join(t.TempDir(), "broadcasters.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	firstID, err := database.CreateSessionWithBroadcaster("movie_a", 10, "", Broadcaster{ID: "owner_a", ScreenID: "alice", Name: "Alice"})
	if err != nil {
		t.Fatal(err)
	}
	secondID, err := database.CreateSessionWithBroadcaster("movie_b", 10, "", Broadcaster{ID: "owner_b", ScreenID: "bob", Name: "Bob"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.AddSnapshot(firstID, &api.MovieSnapshot{IsLive: true, CurrentViewCount: 100}, 0, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := database.AddSnapshot(secondID, &api.MovieSnapshot{IsLive: true, CurrentViewCount: 1000}, 0, 0); err != nil {
		t.Fatal(err)
	}

	broadcasters, err := database.ListBroadcasters()
	if err != nil {
		t.Fatal(err)
	}
	if len(broadcasters) != 2 {
		t.Fatalf("expected two broadcasters, got %+v", broadcasters)
	}

	movies, err := database.ListMoviesByBroadcaster("owner_a")
	if err != nil {
		t.Fatal(err)
	}
	if len(movies) != 1 || movies[0].MovieID != "movie_a" {
		t.Fatalf("another broadcaster leaked into movie list: %+v", movies)
	}

	analysis, err := database.GetAnalysisData("owner_a")
	if err != nil {
		t.Fatal(err)
	}
	if len(analysis) != 1 || analysis[0].AvgViewers != 100 || analysis[0].DataPoints != 1 {
		t.Fatalf("another broadcaster leaked into analysis: %+v", analysis)
	}
}

func TestBackfillBroadcasterForMovie(t *testing.T) {
	database, err := New(filepath.Join(t.TempDir(), "backfill.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if _, err := database.CreateSession("movie_old", 10, ""); err != nil {
		t.Fatal(err)
	}
	ids, err := database.ListUnattributedMovieIDs()
	if err != nil || len(ids) != 1 || ids[0] != "movie_old" {
		t.Fatalf("unexpected unattributed IDs: %v, %v", ids, err)
	}
	updated, err := database.BackfillBroadcasterForMovie("movie_old", Broadcaster{ID: "owner", ScreenID: "screen", Name: "Name"})
	if err != nil || updated != 1 {
		t.Fatalf("backfill failed: updated=%d err=%v", updated, err)
	}
	updated, err = database.BackfillBroadcasterForMovie("movie_old", Broadcaster{ID: "owner", ScreenID: "screen", Name: "Name"})
	if err != nil || updated != 0 {
		t.Fatalf("backfill was not idempotent: updated=%d err=%v", updated, err)
	}
}

func TestNewMigratesLegacySessionsTable(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacy.Exec(`
CREATE TABLE sessions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    movie_id TEXT NOT NULL,
    started_at TEXT NOT NULL,
    label TEXT,
    interval_sec INTEGER NOT NULL DEFAULT 10
);
INSERT INTO sessions (movie_id, started_at, interval_sec)
VALUES ('legacy_movie', '2026-08-19T00:00:00Z', 10);
`)
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	database, err := New(dbPath)
	if err != nil {
		t.Fatalf("legacy migration failed: %v", err)
	}
	defer database.Close()
	sessions, err := database.ListSessions("")
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].MovieID != "legacy_movie" || sessions[0].BroadcasterID != "" {
		t.Fatalf("legacy data was not preserved: %+v", sessions)
	}
}

func TestRecordCommentIgnoresDuplicateWithoutIncrementingCommenter(t *testing.T) {
	database, err := New(filepath.Join(t.TempDir(), "comments.db"))
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer database.Close()

	createdAt := time.Now().Unix()
	inserted, err := database.RecordComment("movie_1", "comment_1", "user_1", "screen_1", "User 1", "hello", createdAt)
	if err != nil {
		t.Fatalf("RecordComment failed: %v", err)
	}
	if !inserted {
		t.Fatal("expected first comment to be inserted")
	}

	inserted, err = database.RecordComment("movie_1", "comment_1", "user_1", "screen_1", "User 1", "hello", createdAt)
	if err != nil {
		t.Fatalf("duplicate RecordComment failed: %v", err)
	}
	if inserted {
		t.Fatal("expected duplicate comment to be ignored")
	}

	commenters, err := database.ListCommenters("movie_1")
	if err != nil {
		t.Fatalf("ListCommenters failed: %v", err)
	}
	if len(commenters) != 1 || commenters[0].CommentCount != 1 {
		t.Fatalf("duplicate comment changed commenter count: %+v", commenters)
	}
}

func TestListMoviesUsesLatestSessionMetadata(t *testing.T) {
	database, err := New(filepath.Join(t.TempDir(), "movies.db"))
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer database.Close()

	firstID, err := database.CreateSession("movie_1", 10, "old label")
	if err != nil {
		t.Fatalf("CreateSession first failed: %v", err)
	}
	if err := database.UpdateSessionTitle(firstID, "old title"); err != nil {
		t.Fatalf("UpdateSessionTitle first failed: %v", err)
	}
	if _, err := database.AddSnapshot(firstID, &api.MovieSnapshot{IsLive: true}, 0, 0); err != nil {
		t.Fatalf("AddSnapshot first failed: %v", err)
	}

	latestID, err := database.CreateSession("movie_1", 30, "latest label")
	if err != nil {
		t.Fatalf("CreateSession latest failed: %v", err)
	}
	if err := database.UpdateSessionTitle(latestID, "latest title"); err != nil {
		t.Fatalf("UpdateSessionTitle latest failed: %v", err)
	}
	if _, err := database.AddSnapshot(latestID, &api.MovieSnapshot{IsLive: true}, 0, 0); err != nil {
		t.Fatalf("AddSnapshot latest failed: %v", err)
	}

	movies, err := database.ListMovies()
	if err != nil {
		t.Fatalf("ListMovies failed: %v", err)
	}
	if len(movies) != 1 {
		t.Fatalf("expected one grouped movie, got %d", len(movies))
	}
	got := movies[0]
	if got.Label != "latest label" || got.Title != "latest title" || got.IntervalSec != 30 || got.TotalRecords != 2 {
		t.Fatalf("ListMovies did not use latest metadata and aggregate records: %+v", got)
	}
}
