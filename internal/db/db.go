package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"tc-analyzer/internal/api"
)

const schemaSQL = `
CREATE TABLE IF NOT EXISTS sessions (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    movie_id     TEXT    NOT NULL,
    started_at   TEXT    NOT NULL,
    label        TEXT,
    interval_sec INTEGER NOT NULL DEFAULT 10,
    title        TEXT
);

CREATE TABLE IF NOT EXISTS snapshots (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id          INTEGER NOT NULL REFERENCES sessions(id),
    recorded_at         TEXT    NOT NULL,
    elapsed_sec         INTEGER NOT NULL,
    is_live             INTEGER NOT NULL,
    current_view_count  INTEGER NOT NULL,
    max_view_count      INTEGER NOT NULL,
    total_view_count    INTEGER NOT NULL,
    comment_count       INTEGER NOT NULL,
    comment_delta       INTEGER NOT NULL,
    duration            INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_snapshots_session ON snapshots(session_id, recorded_at);
CREATE INDEX IF NOT EXISTS idx_snapshots_movie   ON snapshots(session_id);

CREATE TABLE IF NOT EXISTS commenters (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    movie_id      TEXT    NOT NULL,
    user_id       TEXT    NOT NULL,
    screen_id     TEXT    NOT NULL,
    name          TEXT    NOT NULL,
    comment_count INTEGER NOT NULL DEFAULT 1,
    first_seen_at TEXT    NOT NULL,
    last_seen_at  TEXT    NOT NULL,
    UNIQUE(movie_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_commenters_movie ON commenters(movie_id);

CREATE TABLE IF NOT EXISTS comment_logs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    movie_id    TEXT    NOT NULL,
    comment_id  TEXT    NOT NULL UNIQUE,
    user_id     TEXT    NOT NULL,
    screen_id   TEXT    NOT NULL,
    message     TEXT    NOT NULL,
    created_at  TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_comment_logs_movie ON comment_logs(movie_id, created_at);
`

// Session represents a single monitoring session.
type Session struct {
	ID          int64     `json:"id"`
	MovieID     string    `json:"movie_id"`
	StartedAt   time.Time `json:"started_at"`
	Label       string    `json:"label"`
	IntervalSec int       `json:"interval_sec"`
	Title       string    `json:"title"`
}

// Snapshot represents a snapshot record in the database.
type Snapshot struct {
	ID                   int64     `json:"id"`
	SessionID            int64     `json:"session_id"`
	RecordedAt           time.Time `json:"recorded_at"`
	ElapsedSec           int       `json:"elapsed_sec"`
	IsLive               bool      `json:"is_live"`
	CurrentViewCount     int       `json:"current_view_count"`
	MaxViewCount         int       `json:"max_view_count"`
	TotalViewCount       int       `json:"total_view_count"`
	CommentCount         int       `json:"comment_count"`
	CommentDelta         int       `json:"comment_delta"`
	Duration             int       `json:"duration"`
	CumulativeCommenters int       `json:"cumulative_commenters"`
}

// SessionSummary holds aggregated statistical summary data.
type SessionSummary struct {
	TotalRecords          int       `json:"total_records"`
	MinViewers            int       `json:"min_viewers"`
	PeakViewers           int       `json:"peak_viewers"`
	AvgViewers            float64   `json:"avg_viewers"`
	SessionMaxView        int       `json:"session_max_view"`
	SessionTotalView      int       `json:"session_total_view"`
	FinalCommentCount     int       `json:"final_comment_count"`
	TotalCommentsObserved int       `json:"total_comments_observed"`
	FirstRecord           time.Time `json:"first_record"`
	LastRecord            time.Time `json:"last_record"`
}

// MovieListRow represents a grouped movie item.
type MovieListRow struct {
	MovieID      string    `json:"movie_id"`
	StartedAt    time.Time `json:"started_at"`
	Label        string    `json:"label"`
	IntervalSec  int       `json:"interval_sec"`
	TotalRecords int       `json:"total_records"`
	Title        string    `json:"title"`
}

// AnalysisRow represents aggregated data by day of week and hour of day (JST).
type AnalysisRow struct {
	DayOfWeek    int     `json:"day_of_week"`
	HourOfDay    int     `json:"hour_of_day"`
	MinuteOfHour int     `json:"minute_of_hour"`
	AvgViewers   float64 `json:"avg_viewers"`
	MaxViewers   int     `json:"max_viewers"`
	DataPoints   int     `json:"data_points"`
}

// Commenter represents a unique user who commented on a movie, with aggregated stats.
type Commenter struct {
	ID           int64     `json:"id"`
	MovieID      string    `json:"movie_id"`
	UserID       string    `json:"user_id"`
	ScreenID     string    `json:"screen_id"`
	Name         string    `json:"name"`
	CommentCount int       `json:"comment_count"`
	FirstSeenAt  time.Time `json:"first_seen_at"`
	LastSeenAt   time.Time `json:"last_seen_at"`
}

// CommentLog represents a single recorded comment.
type CommentLog struct {
	ID        int64     `json:"id"`
	MovieID   string    `json:"movie_id"`
	CommentID string    `json:"comment_id"`
	UserID    string    `json:"user_id"`
	ScreenID  string    `json:"screen_id"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

// DB wraps database connection and operations.
type DB struct {
	path string
	db   *sql.DB
}

// New initializes the database connection and creates required tables.
func New(dbPath string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory for DB: %w", err)
	}

	dsn := fmt.Sprintf("%s?_busy_timeout=5000", dbPath)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	sqlDB.SetMaxOpenConns(1)

	if _, err := sqlDB.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to set WAL mode: %w", err)
	}
	if _, err := sqlDB.Exec("PRAGMA foreign_keys=ON;"); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to set foreign keys: %w", err)
	}

	if _, err := sqlDB.Exec(schemaSQL); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to execute schema SQL: %w", err)
	}

	// Auto-migrate: add title column to sessions if not present
	_, _ = sqlDB.Exec("ALTER TABLE sessions ADD COLUMN title TEXT;")

	return &DB{path: dbPath, db: sqlDB}, nil
}

// Close closes the underlying database connection.
func (d *DB) Close() error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

// CreateSession inserts a new monitoring session.
func (d *DB) CreateSession(movieID string, intervalSec int, label string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	var labelNull sql.NullString
	if label != "" {
		labelNull = sql.NullString{String: label, Valid: true}
	}

	res, err := d.db.Exec(
		"INSERT INTO sessions (movie_id, started_at, label, interval_sec) VALUES (?, ?, ?, ?)",
		movieID, now, labelNull, intervalSec,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateSessionTitle updates the title for a session if empty or not set.
func (d *DB) UpdateSessionTitle(sessionID int64, title string) error {
	if title == "" {
		return nil
	}
	_, err := d.db.Exec("UPDATE sessions SET title = ? WHERE id = ? AND (title IS NULL OR title = '')", title, sessionID)
	return err
}

// GetSession retrieves a session by ID.
func (d *DB) GetSession(sessionID int64) (*Session, error) {
	row := d.db.QueryRow("SELECT id, movie_id, started_at, label, interval_sec, COALESCE(title, '') FROM sessions WHERE id = ?", sessionID)
	return scanSession(row)
}

// ListSessions retrieves all sessions or sessions filtered by movieID.
func (d *DB) ListSessions(movieID string) ([]*Session, error) {
	var rows *sql.Rows
	var err error
	if movieID != "" {
		rows, err = d.db.Query("SELECT id, movie_id, started_at, label, interval_sec, COALESCE(title, '') FROM sessions WHERE movie_id = ? ORDER BY id DESC", movieID)
	} else {
		rows, err = d.db.Query("SELECT id, movie_id, started_at, label, interval_sec, COALESCE(title, '') FROM sessions ORDER BY id DESC")
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		s, err := scanSessionRows(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// GetLatestSessionByMovie returns the newest session for a movie ID.
func (d *DB) GetLatestSessionByMovie(movieID string) (*Session, error) {
	row := d.db.QueryRow("SELECT id, movie_id, started_at, label, interval_sec, COALESCE(title, '') FROM sessions WHERE movie_id = ? ORDER BY id DESC LIMIT 1", movieID)
	return scanSession(row)
}

// ListMovies returns grouped movies.
func (d *DB) ListMovies() ([]*MovieListRow, error) {
	query := `
SELECT 
    s.movie_id,
    MIN(s.started_at) AS started_at,
    COALESCE(MAX(s.label), '') AS label,
    s.interval_sec,
    COUNT(sn.id) AS total_records,
    COALESCE(MAX(s.title), '') AS title
FROM sessions s
LEFT JOIN snapshots sn ON s.id = sn.session_id
GROUP BY s.movie_id
ORDER BY s.id DESC
`
	rows, err := d.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*MovieListRow
	for rows.Next() {
		var r MovieListRow
		var startedAtStr string
		var labelNull sql.NullString
		var titleNull sql.NullString
		if err := rows.Scan(&r.MovieID, &startedAtStr, &labelNull, &r.IntervalSec, &r.TotalRecords, &titleNull); err != nil {
			return nil, err
		}
		if t, err := parseISO(startedAtStr); err == nil {
			r.StartedAt = t
		}
		if labelNull.Valid {
			r.Label = labelNull.String
		}
		if titleNull.Valid {
			r.Title = titleNull.String
		}
		result = append(result, &r)
	}
	return result, rows.Err()
}

// AddSnapshot inserts a snapshot into the database.
func (d *DB) AddSnapshot(sessionID int64, snap *api.MovieSnapshot, elapsedSec int, commentDelta int) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	isLiveInt := 0
	if snap.IsLive {
		isLiveInt = 1
	}

	query := `
INSERT INTO snapshots (
    session_id, recorded_at, elapsed_sec, is_live,
    current_view_count, max_view_count, total_view_count,
    comment_count, comment_delta, duration
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`
	res, err := d.db.Exec(query,
		sessionID, now, elapsedSec, isLiveInt,
		snap.CurrentViewCount, snap.MaxViewCount, snap.TotalViewCount,
		snap.CommentCount, commentDelta, snap.Duration,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetSnapshots returns all snapshots for a given sessionID.
func (d *DB) GetSnapshots(sessionID int64) ([]*Snapshot, error) {
	query := `
SELECT id, session_id, recorded_at, elapsed_sec, is_live,
       current_view_count, max_view_count, total_view_count,
       comment_count, comment_delta, duration
FROM snapshots
WHERE session_id = ?
ORDER BY recorded_at ASC
`
	rows, err := d.db.Query(query, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanSnapshots(rows)
}

// GetLatestSnapshot returns the newest snapshot for a session.
func (d *DB) GetLatestSnapshot(sessionID int64) (*Snapshot, error) {
	query := `
SELECT id, session_id, recorded_at, elapsed_sec, is_live,
       current_view_count, max_view_count, total_view_count,
       comment_count, comment_delta, duration
FROM snapshots
WHERE session_id = ?
ORDER BY id DESC LIMIT 1
`
	row := d.db.QueryRow(query, sessionID)
	return scanSnapshot(row)
}

// GetMovieSnapshots returns all snapshots for a movie_id where is_live = 1.
func (d *DB) GetMovieSnapshots(movieID string) ([]*Snapshot, error) {
	query := `
SELECT sn.id, sn.session_id, sn.recorded_at, sn.elapsed_sec, sn.is_live,
       sn.current_view_count, sn.max_view_count, sn.total_view_count,
       sn.comment_count, sn.comment_delta, sn.duration
FROM snapshots sn
JOIN sessions s ON sn.session_id = s.id
WHERE s.movie_id = ? AND sn.is_live = 1
ORDER BY sn.recorded_at ASC
`
	rows, err := d.db.Query(query, movieID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanSnapshots(rows)
}

// GetSessionSummary calculates statistical summary for a session.
func (d *DB) GetSessionSummary(sessionID int64) (*SessionSummary, error) {
	query := `
SELECT
    COUNT(*),
    COALESCE(MIN(current_view_count), 0),
    COALESCE(MAX(current_view_count), 0),
    COALESCE(AVG(current_view_count), 0.0),
    COALESCE(MAX(max_view_count), 0),
    COALESCE(MAX(total_view_count), 0),
    COALESCE(MAX(comment_count), 0),
    COALESCE(SUM(comment_delta), 0),
    COALESCE(MIN(recorded_at), ''),
    COALESCE(MAX(recorded_at), '')
FROM snapshots
WHERE session_id = ? AND is_live = 1
`
	row := d.db.QueryRow(query, sessionID)
	return scanSummary(row)
}

// GetMovieSummary calculates overall statistical summary for a movie_id across all live snapshots.
func (d *DB) GetMovieSummary(movieID string) (*SessionSummary, error) {
	query := `
SELECT
    COUNT(*),
    COALESCE(MIN(current_view_count), 0),
    COALESCE(MAX(current_view_count), 0),
    COALESCE(AVG(current_view_count), 0.0),
    COALESCE(MAX(
        (SELECT MAX(max_view_count) FROM snapshots WHERE session_id IN (SELECT id FROM sessions WHERE movie_id = ?)),
        MAX(current_view_count)
    ), 0),
    COALESCE(MAX(
        (SELECT MAX(total_view_count) FROM snapshots WHERE session_id IN (SELECT id FROM sessions WHERE movie_id = ?)),
        MAX(total_view_count)
    ), 0),
    COALESCE(MAX(comment_count), 0),
    COALESCE(SUM(comment_delta), 0),
    COALESCE(MIN(recorded_at), ''),
    COALESCE(MAX(recorded_at), '')
FROM snapshots sn
JOIN sessions s ON sn.session_id = s.id
WHERE s.movie_id = ? AND sn.is_live = 1
`
	row := d.db.QueryRow(query, movieID, movieID, movieID)
	return scanSummary(row)
}

// GetAnalysisData generates hourly and day-of-week viewer statistics in JST (+9 hours).
func (d *DB) GetAnalysisData() ([]*AnalysisRow, error) {
	query := `
SELECT 
    CAST(strftime('%w', datetime(recorded_at, '+9 hours')) AS INTEGER) AS day_of_week,
    CAST(strftime('%H', datetime(recorded_at, '+9 hours')) AS INTEGER) AS hour_of_day,
    CASE WHEN CAST(strftime('%M', datetime(recorded_at, '+9 hours')) AS INTEGER) < 30 THEN 0 ELSE 30 END AS minute_of_hour,
    AVG(current_view_count) AS avg_viewers,
    MAX(current_view_count) AS max_viewers,
    COUNT(*) AS data_points
FROM snapshots
WHERE is_live = 1
GROUP BY day_of_week, hour_of_day, minute_of_hour
ORDER BY day_of_week, hour_of_day, minute_of_hour;
`
	rows, err := d.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*AnalysisRow
	for rows.Next() {
		var a AnalysisRow
		if err := rows.Scan(&a.DayOfWeek, &a.HourOfDay, &a.MinuteOfHour, &a.AvgViewers, &a.MaxViewers, &a.DataPoints); err != nil {
			return nil, err
		}
		list = append(list, &a)
	}
	return list, rows.Err()
}

// Helper functions for scanning

func scanSession(row *sql.Row) (*Session, error) {
	var s Session
	var startedAtStr string
	var labelNull sql.NullString
	var titleNull sql.NullString
	if err := row.Scan(&s.ID, &s.MovieID, &startedAtStr, &labelNull, &s.IntervalSec, &titleNull); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if t, err := parseISO(startedAtStr); err == nil {
		s.StartedAt = t
	}
	if labelNull.Valid {
		s.Label = labelNull.String
	}
	if titleNull.Valid {
		s.Title = titleNull.String
	}
	return &s, nil
}

func scanSessionRows(rows *sql.Rows) (*Session, error) {
	var s Session
	var startedAtStr string
	var labelNull sql.NullString
	var titleNull sql.NullString
	if err := rows.Scan(&s.ID, &s.MovieID, &startedAtStr, &labelNull, &s.IntervalSec, &titleNull); err != nil {
		return nil, err
	}
	if t, err := parseISO(startedAtStr); err == nil {
		s.StartedAt = t
	}
	if labelNull.Valid {
		s.Label = labelNull.String
	}
	if titleNull.Valid {
		s.Title = titleNull.String
	}
	return &s, nil
}

func scanSnapshot(row *sql.Row) (*Snapshot, error) {
	var sn Snapshot
	var recordedAtStr string
	var isLiveInt int
	if err := row.Scan(&sn.ID, &sn.SessionID, &recordedAtStr, &sn.ElapsedSec, &isLiveInt,
		&sn.CurrentViewCount, &sn.MaxViewCount, &sn.TotalViewCount,
		&sn.CommentCount, &sn.CommentDelta, &sn.Duration); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if t, err := parseISO(recordedAtStr); err == nil {
		sn.RecordedAt = t
	}
	sn.IsLive = (isLiveInt == 1)
	return &sn, nil
}

func scanSnapshots(rows *sql.Rows) ([]*Snapshot, error) {
	var snapshots []*Snapshot
	for rows.Next() {
		var sn Snapshot
		var recordedAtStr string
		var isLiveInt int
		if err := rows.Scan(&sn.ID, &sn.SessionID, &recordedAtStr, &sn.ElapsedSec, &isLiveInt,
			&sn.CurrentViewCount, &sn.MaxViewCount, &sn.TotalViewCount,
			&sn.CommentCount, &sn.CommentDelta, &sn.Duration); err != nil {
			return nil, err
		}
		if t, err := parseISO(recordedAtStr); err == nil {
			sn.RecordedAt = t
		}
		sn.IsLive = (isLiveInt == 1)
		snapshots = append(snapshots, &sn)
	}
	return snapshots, rows.Err()
}

func scanSummary(row *sql.Row) (*SessionSummary, error) {
	var sum SessionSummary
	var firstStr, lastStr string
	if err := row.Scan(
		&sum.TotalRecords, &sum.MinViewers, &sum.PeakViewers, &sum.AvgViewers,
		&sum.SessionMaxView, &sum.SessionTotalView, &sum.FinalCommentCount,
		&sum.TotalCommentsObserved, &firstStr, &lastStr,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if t, err := parseISO(firstStr); err == nil {
		sum.FirstRecord = t
	}
	if t, err := parseISO(lastStr); err == nil {
		sum.LastRecord = t
	}
	return &sum, nil
}

func parseISO(iso string) (time.Time, error) {
	if iso == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, iso)
}

// UpsertCommenter inserts or updates a commenter record for a movie.
// On conflict (same movie_id + user_id) it increments comment_count and updates last_seen_at and name.
func (d *DB) UpsertCommenter(movieID, userID, screenID, name string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.db.Exec(`
INSERT INTO commenters (movie_id, user_id, screen_id, name, comment_count, first_seen_at, last_seen_at)
VALUES (?, ?, ?, ?, 1, ?, ?)
ON CONFLICT(movie_id, user_id) DO UPDATE SET
    comment_count = comment_count + 1,
    last_seen_at  = excluded.last_seen_at,
    name          = excluded.name,
    screen_id     = excluded.screen_id
`, movieID, userID, screenID, name, now, now)
	return err
}

// AddCommentLog inserts a single comment log entry. Silently ignores duplicate comment_id.
func (d *DB) AddCommentLog(movieID, commentID, userID, screenID, message string, createdAt int64) error {
	ts := time.Unix(createdAt, 0).UTC().Format(time.RFC3339)
	_, err := d.db.Exec(`
INSERT OR IGNORE INTO comment_logs (movie_id, comment_id, user_id, screen_id, message, created_at)
VALUES (?, ?, ?, ?, ?, ?)
`, movieID, commentID, userID, screenID, message, ts)
	return err
}

// ListCommenters returns all commenters for a movie, ordered by comment count descending.
func (d *DB) ListCommenters(movieID string) ([]*Commenter, error) {
	rows, err := d.db.Query(`
SELECT id, movie_id, user_id, screen_id, name, comment_count, first_seen_at, last_seen_at
FROM commenters
WHERE movie_id = ?
ORDER BY comment_count DESC, first_seen_at ASC
`, movieID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*Commenter
	for rows.Next() {
		var c Commenter
		var firstStr, lastStr string
		if err := rows.Scan(&c.ID, &c.MovieID, &c.UserID, &c.ScreenID, &c.Name, &c.CommentCount, &firstStr, &lastStr); err != nil {
			return nil, err
		}
		if t, err := parseISO(firstStr); err == nil {
			c.FirstSeenAt = t
		}
		if t, err := parseISO(lastStr); err == nil {
			c.LastSeenAt = t
		}
		list = append(list, &c)
	}
	return list, rows.Err()
}

// GetCommenterCount returns the number of unique commenters for a movie.
func (d *DB) GetCommenterCount(movieID string) (int, error) {
	var count int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM commenters WHERE movie_id = ?`, movieID).Scan(&count)
	return count, err
}
