package api

import (
	"context"
	"net/http"
	"net/http/httptest"
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
