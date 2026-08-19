package dashboard

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"sort"
	"strconv"
	"time"

	"tc-analyzer/internal/db"
)

//go:embed assets
var frontendAssets embed.FS

// MovieDetailResponse represents the detail data for a single movie.
type MovieDetailResponse struct {
	Movie          *db.Session        `json:"movie"`
	Summary        *db.SessionSummary `json:"summary"`
	Snapshots      []*db.Snapshot     `json:"snapshots"`
	CommenterCount int                `json:"commenter_count"`
}

// Server handles HTTP web dashboard requests.
type Server struct {
	db   *db.DB
	addr string
}

// NewServer creates a new dashboard server.
func NewServer(dbClient *db.DB, host string, port int) *Server {
	return &Server{
		db:   dbClient,
		addr: net.JoinHostPort(host, strconv.Itoa(port)),
	}
}

// Start launches the HTTP server and blocks until context cancellation or error.
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	staticFS, err := fs.Sub(frontendAssets, "assets")
	if err != nil {
		return fmt.Errorf("failed to create static fs: %w", err)
	}

	mux.Handle("GET /", http.FileServer(http.FS(staticFS)))
	mux.HandleFunc("GET /api/broadcasters", s.handleGetBroadcasters)
	mux.HandleFunc("GET /api/movies", s.handleGetMovies)
	mux.HandleFunc("GET /api/movies/{movie_id}", s.handleGetMovieDetail)
	mux.HandleFunc("GET /api/movies/{movie_id}/commenters", s.handleGetCommenters)
	mux.HandleFunc("GET /api/analysis", s.handleGetAnalysis)

	server := &http.Server{
		Addr:    s.addr,
		Handler: loggerMiddleware(mux),
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdownCtx)
	}()

	log.Printf("Starting dashboard server on http://%s\n", s.addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *Server) handleGetMovies(w http.ResponseWriter, r *http.Request) {
	broadcasterID := r.URL.Query().Get("broadcaster_id")
	if broadcasterID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "broadcaster_id parameter required"})
		return
	}
	exists, err := s.db.BroadcasterExists(broadcasterID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !exists {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Broadcaster not found"})
		return
	}
	movies, err := s.db.ListMoviesByBroadcaster(broadcasterID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": %q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	if movies == nil {
		movies = []*db.MovieListRow{}
	}
	writeJSON(w, http.StatusOK, movies)
}

func (s *Server) handleGetBroadcasters(w http.ResponseWriter, r *http.Request) {
	broadcasters, err := s.db.ListBroadcasters()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if broadcasters == nil {
		broadcasters = []*db.BroadcasterListRow{}
	}
	writeJSON(w, http.StatusOK, broadcasters)
}

func (s *Server) handleGetMovieDetail(w http.ResponseWriter, r *http.Request) {
	movieID := r.PathValue("movie_id")
	if movieID == "" {
		http.Error(w, `{"error": "movie_id parameter required"}`, http.StatusBadRequest)
		return
	}

	session, err := s.db.GetLatestSessionByMovie(movieID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": %q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	if session == nil {
		http.Error(w, `{"error": "Movie not found"}`, http.StatusNotFound)
		return
	}

	snapshots, err := s.db.GetMovieSnapshots(movieID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": %q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	if snapshots == nil {
		snapshots = []*db.Snapshot{}
	}

	summary, err := s.db.GetMovieSummary(movieID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": %q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	commenters, err := s.db.ListCommenters(movieID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": %q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	var firstSeenList []time.Time
	for _, c := range commenters {
		if !c.FirstSeenAt.IsZero() {
			firstSeenList = append(firstSeenList, c.FirstSeenAt)
		}
	}
	sort.Slice(firstSeenList, func(i, j int) bool {
		return firstSeenList[i].Before(firstSeenList[j])
	})

	idx := 0
	for _, snap := range snapshots {
		for idx < len(firstSeenList) && !firstSeenList[idx].After(snap.RecordedAt) {
			idx++
		}
		snap.CumulativeCommenters = idx
	}

	resp := MovieDetailResponse{
		Movie:          session,
		Summary:        summary,
		Snapshots:      snapshots,
		CommenterCount: len(commenters),
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGetCommenters(w http.ResponseWriter, r *http.Request) {
	movieID := r.PathValue("movie_id")
	if movieID == "" {
		http.Error(w, `{"error": "movie_id parameter required"}`, http.StatusBadRequest)
		return
	}

	commenters, err := s.db.ListCommenters(movieID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": %q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	if commenters == nil {
		commenters = []*db.Commenter{}
	}
	writeJSON(w, http.StatusOK, commenters)
}

func (s *Server) handleGetAnalysis(w http.ResponseWriter, r *http.Request) {
	broadcasterID := r.URL.Query().Get("broadcaster_id")
	if broadcasterID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "broadcaster_id parameter required"})
		return
	}
	exists, err := s.db.BroadcasterExists(broadcasterID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !exists {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Broadcaster not found"})
		return
	}
	data, err := s.db.GetAnalysisData(broadcasterID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": %q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	if data == nil {
		data = []*db.AnalysisRow{}
	}
	writeJSON(w, http.StatusOK, data)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func loggerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}
