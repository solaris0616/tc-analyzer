package db

import (
	"path/filepath"
	"testing"

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
	sessionID, err := database.CreateSession("movie_100", 10, "Test Session")
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
	analysis, err := database.GetAnalysisData()
	if err != nil {
		t.Fatalf("GetAnalysisData failed: %v", err)
	}
	if len(analysis) == 0 {
		t.Errorf("expected analysis data, got empty")
	}
}
