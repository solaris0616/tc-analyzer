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
    title        TEXT,
    broadcaster_id        TEXT,
    broadcaster_screen_id TEXT,
    broadcaster_name      TEXT
);

CREATE TABLE IF NOT EXISTS snapshots (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id          INTEGER NOT NULL REFERENCES sessions(id),
    recorded_at         TEXT    NOT NULL,
    current_view_count  INTEGER NOT NULL,
    max_view_count      INTEGER NOT NULL,
    total_view_count    INTEGER NOT NULL,
    comment_count       INTEGER NOT NULL,
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
	ID                  int64     `json:"id"`
	MovieID             string    `json:"movie_id"`
	StartedAt           time.Time `json:"started_at"`
	Label               string    `json:"label"`
	Title               string    `json:"title"`
	BroadcasterID       string    `json:"broadcaster_id"`
	BroadcasterScreenID string    `json:"broadcaster_screen_id"`
	BroadcasterName     string    `json:"broadcaster_name"`
}

// Snapshot represents a snapshot record in the database.
type Snapshot struct {
	ID                   int64     `json:"id"`
	SessionID            int64     `json:"session_id"`
	RecordedAt           time.Time `json:"recorded_at"`
	CurrentViewCount     int       `json:"current_view_count"`
	MaxViewCount         int       `json:"max_view_count"`
	TotalViewCount       int       `json:"total_view_count"`
	CommentCount         int       `json:"comment_count"`
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
	TotalRecords int       `json:"total_records"`
	Title        string    `json:"title"`
}

// Broadcaster identifies the owner of a monitored movie.
type Broadcaster struct {
	ID       string `json:"id"`
	ScreenID string `json:"screen_id"`
	Name     string `json:"name"`
}

// BroadcasterListRow contains dashboard selector metadata.
type BroadcasterListRow struct {
	ID           string    `json:"id"`
	ScreenID     string    `json:"screen_id"`
	Name         string    `json:"name"`
	MovieCount   int       `json:"movie_count"`
	SessionCount int       `json:"session_count"`
	LastSeenAt   time.Time `json:"last_seen_at"`
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

	if _, err := sqlDB.Exec(`CREATE INDEX IF NOT EXISTS idx_sessions_broadcaster ON sessions(broadcaster_id, movie_id, started_at)`); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to create broadcaster index: %w", err)
	}

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
func (d *DB) CreateSession(movieID string, label string) (int64, error) {
	return d.CreateSessionWithBroadcaster(movieID, label, Broadcaster{})
}

// CreateSessionWithBroadcaster inserts a monitoring session with its owner.
func (d *DB) CreateSessionWithBroadcaster(movieID string, label string, broadcaster Broadcaster) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	var labelNull sql.NullString
	if label != "" {
		labelNull = sql.NullString{String: label, Valid: true}
	}

	res, err := d.db.Exec(
		`INSERT INTO sessions
         (movie_id, started_at, label, broadcaster_id, broadcaster_screen_id, broadcaster_name)
         VALUES (?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''))`,
		movieID, now, labelNull, broadcaster.ID, broadcaster.ScreenID, broadcaster.Name,
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
	row := d.db.QueryRow("SELECT id, movie_id, started_at, label, COALESCE(title, ''), COALESCE(broadcaster_id, ''), COALESCE(broadcaster_screen_id, ''), COALESCE(broadcaster_name, '') FROM sessions WHERE id = ?", sessionID)
	return scanSession(row)
}

// ListSessions retrieves all sessions or sessions filtered by movieID.
func (d *DB) ListSessions(movieID string) ([]*Session, error) {
	var rows *sql.Rows
	var err error
	if movieID != "" {
		rows, err = d.db.Query("SELECT id, movie_id, started_at, label, COALESCE(title, ''), COALESCE(broadcaster_id, ''), COALESCE(broadcaster_screen_id, ''), COALESCE(broadcaster_name, '') FROM sessions WHERE movie_id = ? ORDER BY id DESC", movieID)
	} else {
		rows, err = d.db.Query("SELECT id, movie_id, started_at, label, COALESCE(title, ''), COALESCE(broadcaster_id, ''), COALESCE(broadcaster_screen_id, ''), COALESCE(broadcaster_name, '') FROM sessions ORDER BY id DESC")
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
	row := d.db.QueryRow("SELECT id, movie_id, started_at, label, COALESCE(title, ''), COALESCE(broadcaster_id, ''), COALESCE(broadcaster_screen_id, ''), COALESCE(broadcaster_name, '') FROM sessions WHERE movie_id = ? ORDER BY id DESC LIMIT 1", movieID)
	return scanSession(row)
}

// ListMovies returns grouped movies.
func (d *DB) ListMovies() ([]*MovieListRow, error) {
	query := `
WITH latest_sessions AS (
    SELECT s.*
    FROM sessions s
    JOIN (
        SELECT movie_id, MAX(id) AS id
        FROM sessions
        GROUP BY movie_id
    ) latest ON latest.id = s.id
), movie_totals AS (
    SELECT
        s.movie_id,
        MIN(s.started_at) AS started_at,
        COUNT(sn.id) AS total_records
    FROM sessions s
    LEFT JOIN snapshots sn ON sn.session_id = s.id
    GROUP BY s.movie_id
)
SELECT
    latest.movie_id,
    totals.started_at,
    COALESCE(latest.label, ''),
    totals.total_records,
    COALESCE(latest.title, '')
FROM latest_sessions latest
JOIN movie_totals totals ON totals.movie_id = latest.movie_id
ORDER BY latest.id DESC
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
		if err := rows.Scan(&r.MovieID, &startedAtStr, &labelNull, &r.TotalRecords, &titleNull); err != nil {
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

func scanMovieListRows(rows *sql.Rows) ([]*MovieListRow, error) {
	var result []*MovieListRow
	for rows.Next() {
		var item MovieListRow
		var startedAtStr string
		var labelNull, titleNull sql.NullString
		if err := rows.Scan(&item.MovieID, &startedAtStr, &labelNull, &item.TotalRecords, &titleNull); err != nil {
			return nil, err
		}
		if parsed, err := parseISO(startedAtStr); err == nil {
			item.StartedAt = parsed
		}
		if labelNull.Valid {
			item.Label = labelNull.String
		}
		if titleNull.Valid {
			item.Title = titleNull.String
		}
		result = append(result, &item)
	}
	return result, rows.Err()
}

// ListMoviesByBroadcaster returns movies owned by one broadcaster.
func (d *DB) ListMoviesByBroadcaster(broadcasterID string) ([]*MovieListRow, error) {
	query := `
WITH filtered_sessions AS (
    SELECT * FROM sessions WHERE broadcaster_id = ?
), latest_sessions AS (
    SELECT s.*
    FROM filtered_sessions s
    JOIN (
        SELECT movie_id, MAX(id) AS id
        FROM filtered_sessions
        GROUP BY movie_id
    ) latest ON latest.id = s.id
), movie_totals AS (
    SELECT
        s.movie_id,
        MIN(s.started_at) AS started_at,
        COUNT(sn.id) AS total_records
    FROM filtered_sessions s
    LEFT JOIN snapshots sn ON sn.session_id = s.id
    GROUP BY s.movie_id
)
SELECT
    latest.movie_id,
    totals.started_at,
    COALESCE(latest.label, ''),
    totals.total_records,
    COALESCE(latest.title, '')
FROM latest_sessions latest
JOIN movie_totals totals ON totals.movie_id = latest.movie_id
ORDER BY latest.id DESC
`
	rows, err := d.db.Query(query, broadcasterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMovieListRows(rows)
}

// ListBroadcasters returns known broadcasters ordered by most recent session.
func (d *DB) ListBroadcasters() ([]*BroadcasterListRow, error) {
	rows, err := d.db.Query(`
WITH aggregates AS (
    SELECT broadcaster_id,
           COUNT(DISTINCT movie_id) AS movie_count,
           COUNT(*) AS session_count,
           MAX(id) AS latest_id,
           MAX(started_at) AS last_seen_at
    FROM sessions
    WHERE broadcaster_id IS NOT NULL AND broadcaster_id <> ''
    GROUP BY broadcaster_id
)
SELECT a.broadcaster_id,
       COALESCE(s.broadcaster_screen_id, ''),
       COALESCE(s.broadcaster_name, ''),
       a.movie_count,
       a.session_count,
       a.last_seen_at
FROM aggregates a
JOIN sessions s ON s.id = a.latest_id
ORDER BY a.last_seen_at DESC, a.broadcaster_id
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*BroadcasterListRow
	for rows.Next() {
		var item BroadcasterListRow
		var lastSeenAt string
		if err := rows.Scan(&item.ID, &item.ScreenID, &item.Name, &item.MovieCount, &item.SessionCount, &lastSeenAt); err != nil {
			return nil, err
		}
		if parsed, err := parseISO(lastSeenAt); err == nil {
			item.LastSeenAt = parsed
		}
		result = append(result, &item)
	}
	return result, rows.Err()
}

// BroadcasterExists reports whether at least one session belongs to the ID.
func (d *DB) BroadcasterExists(broadcasterID string) (bool, error) {
	var exists int
	err := d.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM sessions WHERE broadcaster_id = ?)`, broadcasterID).Scan(&exists)
	return exists == 1, err
}

// ListUnattributedMovieIDs returns movie IDs whose sessions need broadcaster metadata.
func (d *DB) ListUnattributedMovieIDs() ([]string, error) {
	rows, err := d.db.Query(`SELECT DISTINCT movie_id FROM sessions WHERE broadcaster_id IS NULL OR broadcaster_id = '' ORDER BY movie_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// BackfillBroadcasterForMovie attributes every session for a movie atomically.
func (d *DB) BackfillBroadcasterForMovie(movieID string, broadcaster Broadcaster) (int64, error) {
	if broadcaster.ID == "" {
		return 0, fmt.Errorf("broadcaster ID is required")
	}
	res, err := d.db.Exec(`
UPDATE sessions
SET broadcaster_id = ?, broadcaster_screen_id = ?, broadcaster_name = ?
WHERE movie_id = ? AND (broadcaster_id IS NULL OR broadcaster_id = '')
`, broadcaster.ID, broadcaster.ScreenID, broadcaster.Name, movieID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// AddSnapshot inserts a live snapshot into the database.
func (d *DB) AddSnapshot(sessionID int64, snap *api.MovieSnapshot) (int64, error) {
	if !snap.IsLive {
		return 0, fmt.Errorf("offline snapshots cannot be stored")
	}
	now := time.Now().UTC().Format(time.RFC3339)

	query := `
INSERT INTO snapshots (
    session_id, recorded_at,
    current_view_count, max_view_count, total_view_count,
    comment_count, duration
) VALUES (?, ?, ?, ?, ?, ?, ?)
`
	res, err := d.db.Exec(query,
		sessionID, now,
		snap.CurrentViewCount, snap.MaxViewCount, snap.TotalViewCount,
		snap.CommentCount, snap.Duration,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetSnapshots returns all snapshots for a given sessionID.
func (d *DB) GetSnapshots(sessionID int64) ([]*Snapshot, error) {
	query := `
SELECT id, session_id, recorded_at,
       current_view_count, max_view_count, total_view_count,
       comment_count, duration
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
SELECT id, session_id, recorded_at,
       current_view_count, max_view_count, total_view_count,
       comment_count, duration
FROM snapshots
WHERE session_id = ?
ORDER BY id DESC LIMIT 1
`
	row := d.db.QueryRow(query, sessionID)
	return scanSnapshot(row)
}

// GetMovieSnapshots returns all snapshots for a movie_id.
func (d *DB) GetMovieSnapshots(movieID string) ([]*Snapshot, error) {
	query := `
SELECT sn.id, sn.session_id, sn.recorded_at,
       sn.current_view_count, sn.max_view_count, sn.total_view_count,
       sn.comment_count, sn.duration
FROM snapshots sn
JOIN sessions s ON sn.session_id = s.id
WHERE s.movie_id = ?
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
WITH ordered AS (
    SELECT snapshots.*,
           LAG(comment_count) OVER (ORDER BY recorded_at, id) AS previous_comment_count
    FROM snapshots
    WHERE session_id = ?
)
SELECT
    COUNT(*),
    COALESCE(MIN(current_view_count), 0),
    COALESCE(MAX(current_view_count), 0),
    COALESCE(AVG(current_view_count), 0.0),
    COALESCE(MAX(max_view_count), 0),
    COALESCE(MAX(total_view_count), 0),
    COALESCE(MAX(comment_count), 0),
    COALESCE(SUM(CASE
        WHEN previous_comment_count IS NOT NULL AND comment_count > previous_comment_count
        THEN comment_count - previous_comment_count
        ELSE 0
    END), 0),
    COALESCE(MIN(recorded_at), ''),
    COALESCE(MAX(recorded_at), '')
FROM ordered
`
	row := d.db.QueryRow(query, sessionID)
	return scanSummary(row)
}

// GetMovieSummary calculates overall statistical summary for a movie_id across all live snapshots.
func (d *DB) GetMovieSummary(movieID string) (*SessionSummary, error) {
	query := `
WITH ordered AS (
    SELECT sn.*,
           LAG(sn.comment_count) OVER (PARTITION BY sn.session_id ORDER BY sn.recorded_at, sn.id) AS previous_comment_count
    FROM snapshots sn
    JOIN sessions s ON sn.session_id = s.id
    WHERE s.movie_id = ?
)
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
    COALESCE(SUM(CASE
        WHEN previous_comment_count IS NOT NULL AND comment_count > previous_comment_count
        THEN comment_count - previous_comment_count
        ELSE 0
    END), 0),
    COALESCE(MIN(recorded_at), ''),
    COALESCE(MAX(recorded_at), '')
FROM ordered
`
	row := d.db.QueryRow(query, movieID, movieID, movieID)
	return scanSummary(row)
}

// GetAnalysisData generates viewer statistics for one broadcaster in JST (+9 hours).
func (d *DB) GetAnalysisData(broadcasterID string) ([]*AnalysisRow, error) {
	if broadcasterID == "" {
		return nil, fmt.Errorf("broadcaster ID is required")
	}
	query := `
SELECT 
    CAST(strftime('%w', datetime(sn.recorded_at, '+9 hours')) AS INTEGER) AS day_of_week,
    CAST(strftime('%H', datetime(sn.recorded_at, '+9 hours')) AS INTEGER) AS hour_of_day,
    CASE WHEN CAST(strftime('%M', datetime(sn.recorded_at, '+9 hours')) AS INTEGER) < 30 THEN 0 ELSE 30 END AS minute_of_hour,
    AVG(sn.current_view_count) AS avg_viewers,
    MAX(sn.current_view_count) AS max_viewers,
    COUNT(*) AS data_points
FROM snapshots sn
JOIN sessions s ON s.id = sn.session_id
WHERE s.broadcaster_id = ?
GROUP BY day_of_week, hour_of_day, minute_of_hour
ORDER BY day_of_week, hour_of_day, minute_of_hour;
`
	rows, err := d.db.Query(query, broadcasterID)
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
	if err := row.Scan(&s.ID, &s.MovieID, &startedAtStr, &labelNull, &titleNull, &s.BroadcasterID, &s.BroadcasterScreenID, &s.BroadcasterName); err != nil {
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
	if err := rows.Scan(&s.ID, &s.MovieID, &startedAtStr, &labelNull, &titleNull, &s.BroadcasterID, &s.BroadcasterScreenID, &s.BroadcasterName); err != nil {
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
	if err := row.Scan(&sn.ID, &sn.SessionID, &recordedAtStr,
		&sn.CurrentViewCount, &sn.MaxViewCount, &sn.TotalViewCount,
		&sn.CommentCount, &sn.Duration); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if t, err := parseISO(recordedAtStr); err == nil {
		sn.RecordedAt = t
	}
	return &sn, nil
}

func scanSnapshots(rows *sql.Rows) ([]*Snapshot, error) {
	var snapshots []*Snapshot
	for rows.Next() {
		var sn Snapshot
		var recordedAtStr string
		if err := rows.Scan(&sn.ID, &sn.SessionID, &recordedAtStr,
			&sn.CurrentViewCount, &sn.MaxViewCount, &sn.TotalViewCount,
			&sn.CommentCount, &sn.Duration); err != nil {
			return nil, err
		}
		if t, err := parseISO(recordedAtStr); err == nil {
			sn.RecordedAt = t
		}
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

// RecordComment atomically inserts a comment log and updates its commenter stats.
// Duplicate comment IDs are ignored without incrementing the commenter count.
func (d *DB) RecordComment(movieID, commentID, userID, screenID, name, message string, createdAt int64) (bool, error) {
	tx, err := d.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	createdAtStr := time.Unix(createdAt, 0).UTC().Format(time.RFC3339)
	res, err := tx.Exec(`
INSERT OR IGNORE INTO comment_logs (movie_id, comment_id, user_id, screen_id, message, created_at)
VALUES (?, ?, ?, ?, ?, ?)
`, movieID, commentID, userID, screenID, message, createdAtStr)
	if err != nil {
		return false, err
	}
	inserted, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if inserted == 0 {
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return false, nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.Exec(`
INSERT INTO commenters (movie_id, user_id, screen_id, name, comment_count, first_seen_at, last_seen_at)
VALUES (?, ?, ?, ?, 1, ?, ?)
ON CONFLICT(movie_id, user_id) DO UPDATE SET
    comment_count = comment_count + 1,
    last_seen_at  = excluded.last_seen_at,
    name          = excluded.name,
    screen_id     = excluded.screen_id
`, movieID, userID, screenID, name, now, now); err != nil {
		return false, err
	}

	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
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
