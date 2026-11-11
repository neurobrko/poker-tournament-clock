// Package store owns the tournament SQLite database and its exclusive app lock.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"git.mpx.sk/elvis/poker_clock/internal/config"
	"github.com/gofrs/flock"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
	_ "modernc.org/sqlite"
)

const (
	// MaxNameLength caps display names accepted by the app.
	MaxNameLength         = config.MaxPlayerNameLength
	currentSchemaVersion  = 5
	databaseApplicationID = 1346587723 // ASCII "PCLK".
)

var (
	ErrDatabaseInUse         = errors.New("tournament database is already open in another application instance")
	ErrDatabaseExists        = errors.New("tournament database already exists")
	ErrNotTournamentDatabase = errors.New("file is not an initialized poker clock tournament database")
	ErrConfigurationLocked   = errors.New("tournament configuration is locked after play starts")
	ErrCheckpointRegression  = errors.New("clock checkpoint cannot move elapsed time backwards")
	ErrUnsupportedSchema     = errors.New("tournament database schema is newer than this application")
	ErrStoreClosed           = errors.New("tournament database is closed")
)

//go:embed migrations/*.sql
var migrationFS embed.FS

var migrationFiles = []string{
	"migrations/001_initial.sql",
	"migrations/002_domain_events.sql",
	"migrations/003_bust_corrections.sql",
	"migrations/004_results.sql",
	"migrations/005_player_aliases.sql",
}

// Store keeps the application lock for the lifetime of its database connection.
type Store struct {
	db   *sql.DB
	lock *flock.Flock
	path string
}

type TournamentStatus string

const (
	StatusNotStarted TournamentStatus = "not_started"
	StatusRunning    TournamentStatus = "running"
	StatusPaused     TournamentStatus = "paused"
	StatusEnded      TournamentStatus = "ended"
)

type TournamentRecord struct {
	Name               string
	Status             TournamentStatus
	Config             config.Template
	CreatedAt          time.Time
	UpdatedAt          time.Time
	StartedAt          *time.Time
	EndedAt            *time.Time
	Checkpoint         Checkpoint
	RegistrationClosed bool
	ResultCompleted    *bool
}

type Checkpoint struct {
	ClockElapsed    time.Duration
	RealElapsed     time.Duration
	SegmentIndex    int // One-based schedule position; zero means no segment selected.
	Remaining       time.Duration
	Running         bool
	SegmentFinished bool
	HoldReason      string
	PendingIndex    int
}

type EventInput struct {
	Kind          string
	Payload       any
	Explanation   string
	ParentEventID *int64
	VoidEventID   *int64
	WallTime      time.Time
	ClockElapsed  *time.Duration
	RealElapsed   *time.Duration
}

type EventRecord struct {
	ID            int64
	Kind          string
	WallTime      time.Time
	ClockElapsed  *time.Duration
	RealElapsed   *time.Duration
	Payload       json.RawMessage
	Explanation   string
	ParentEventID *int64
	VoidEventID   *int64
}

// Create makes a new database without overwriting an existing path, initializes its schema,
// and imports the initial tournament configuration in the same audit history.
func Create(ctx context.Context, path, name string, template config.Template) (*Store, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("tournament name is required")
	}
	if len([]rune(name)) > MaxNameLength {
		return nil, fmt.Errorf("tournament name cannot exceed %d characters", MaxNameLength)
	}
	if err := template.Validate(); err != nil {
		return nil, fmt.Errorf("invalid tournament template: %w", err)
	}
	canonical, err := canonicalPath(path, true)
	if err != nil {
		return nil, err
	}
	if _, err := os.Lstat(canonical); err == nil {
		return nil, fmt.Errorf("%w: %s", ErrDatabaseExists, canonical)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect database destination: %w", err)
	}
	lock, err := acquireLock(canonical)
	if err != nil {
		return nil, err
	}
	createdFile, err := os.OpenFile(canonical, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		_ = lock.Close()
		if errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("%w: %s", ErrDatabaseExists, canonical)
		}
		return nil, fmt.Errorf("create tournament database: %w", err)
	}
	if err := createdFile.Close(); err != nil {
		_ = lock.Close()
		_ = os.Remove(canonical)
		return nil, fmt.Errorf("close newly created database file: %w", err)
	}

	db, err := openDB(ctx, canonical, true)
	if err != nil {
		_ = lock.Close()
		removeDatabaseFiles(canonical)
		return nil, err
	}
	store := &Store{db: db, lock: lock, path: canonical}
	if err := store.initializeTournament(ctx, name, template); err != nil {
		_ = store.Close()
		removeDatabaseFiles(canonical)
		return nil, err
	}
	return store, nil
}

// Open acquires the per-database app lock and opens an existing tournament.
func Open(ctx context.Context, path string) (*Store, error) {
	canonical, err := canonicalPath(path, false)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return nil, fmt.Errorf("inspect tournament database: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("tournament database path is not a regular file")
	}
	lock, err := acquireLock(canonical)
	if err != nil {
		return nil, err
	}
	db, err := openDB(ctx, canonical, false)
	if err != nil {
		_ = lock.Close()
		return nil, err
	}
	store := &Store{db: db, lock: lock, path: canonical}
	if _, err := store.Tournament(ctx); err != nil {
		_ = store.Close()
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotTournamentDatabase
		}
		return nil, fmt.Errorf("read tournament database: %w", err)
	}
	if err := store.recoverOnOpen(ctx); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("recover tournament clock: %w", err)
	}
	return store, nil
}

func acquireLock(databasePath string) (*flock.Flock, error) {
	lockPath := databasePath + ".lock"
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open tournament lock file: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close tournament lock file: %w", err)
	}
	lock := flock.New(lockPath, flock.SetPermissions(0o600))
	locked, err := lock.TryLock()
	if err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("lock tournament database: %w", err)
	}
	if !locked {
		_ = lock.Close()
		return nil, ErrDatabaseInUse
	}
	return lock, nil
}

func canonicalPath(path string, allowMissing bool) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("database path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve database path: %w", err)
	}
	absolute = filepath.Clean(absolute)
	if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
		return resolved, nil
	} else if !errors.Is(err, os.ErrNotExist) || !allowMissing {
		return "", fmt.Errorf("resolve database path: %w", err)
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return "", fmt.Errorf("resolve database directory: %w", err)
	}
	return filepath.Join(parent, filepath.Base(absolute)), nil
}

func openDB(ctx context.Context, path string, allowInitialization bool) (*sql.DB, error) {
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		return nil, fmt.Errorf("open tournament database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect to tournament database: %w", err)
	}
	for _, pragma := range []string{"PRAGMA busy_timeout = 5000", "PRAGMA foreign_keys = ON"} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("configure tournament database (%s): %w", pragma, err)
		}
	}
	var applicationID int
	if err := db.QueryRowContext(ctx, "PRAGMA application_id").Scan(&applicationID); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("read database application ID: %w", err)
	}
	if applicationID != databaseApplicationID && !(allowInitialization && applicationID == 0) {
		_ = db.Close()
		return nil, ErrNotTournamentDatabase
	}
	for _, pragma := range []string{"PRAGMA journal_mode = WAL", "PRAGMA synchronous = FULL"} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("configure tournament database (%s): %w", pragma, err)
		}
	}
	if err := migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func sqliteDSN(path string) string {
	filePath := filepath.ToSlash(path)
	if filepath.VolumeName(path) != "" && !strings.HasPrefix(filePath, "/") {
		filePath = "/" + filePath
	}
	uri := url.URL{Scheme: "file", Path: filePath}
	return uri.String() + "?mode=rw"
}

func migrate(ctx context.Context, db *sql.DB) error {
	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read database schema version: %w", err)
	}
	if version > currentSchemaVersion {
		return fmt.Errorf("%w: found %d, supported through %d", ErrUnsupportedSchema, version, currentSchemaVersion)
	}
	for next := version + 1; next <= currentSchemaVersion; next++ {
		if next > len(migrationFiles) {
			return fmt.Errorf("missing database migration %d", next)
		}
		migration, err := migrationFS.ReadFile(migrationFiles[next-1])
		if err != nil {
			return fmt.Errorf("read database migration %d: %w", next, err)
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin database migration %d: %w", next, err)
		}
		if _, err := tx.ExecContext(ctx, string(migration)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply database migration %d: %w", next, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit database migration %d: %w", next, err)
		}
		version = next
	}
	return nil
}

func removeDatabaseFiles(path string) {
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		_ = os.Remove(path + suffix)
	}
}

func (s *Store) initializeTournament(ctx context.Context, name string, template config.Template) error {
	configJSON, err := json.Marshal(template)
	if err != nil {
		return fmt.Errorf("encode tournament configuration: %w", err)
	}
	wall := time.Now().In(time.Local)
	wallString := FormatWallTime(wall)
	payload, err := json.Marshal(map[string]any{"name": name, "configuration": json.RawMessage(configJSON)})
	if err != nil {
		return fmt.Errorf("encode tournament creation event: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tournament initialization: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO tournament (id, name, status, configuration_json, created_at, updated_at, remaining_ns)
		VALUES (1, ?, ?, ?, ?, ?, ?)`, name, StatusNotStarted, string(configJSON), wallString, wallString, int64(template.Schedule[0].Duration.Value())); err != nil {
		return fmt.Errorf("initialize tournament record: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events (kind, wall_timestamp, payload_json)
		VALUES ('tournament_created', ?, ?)`, wallString, string(payload)); err != nil {
		return fmt.Errorf("record tournament creation: %w", err)
	}
	for _, player := range template.Players {
		if err := insertSeedPlayer(ctx, tx, wallString, player); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tournament initialization: %w", err)
	}
	return nil
}

func insertSeedPlayer(ctx context.Context, tx *sql.Tx, wallString string, player config.Player) error {
	id, err := newPlayerID()
	if err != nil {
		return err
	}
	name := norm.NFC.String(strings.TrimSpace(player.Name))
	alias := strings.TrimSpace(player.Alias)
	key := norm.NFC.String(cases.Fold().String(name))
	payload, err := json.Marshal(map[string]string{"player_id": id, "name": name, "alias": alias})
	if err != nil {
		return fmt.Errorf("encode seeded player event: %w", err)
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO audit_events (kind, wall_timestamp, payload_json)
		VALUES ('player_created', ?, ?)`, wallString, string(payload))
	if err != nil {
		return fmt.Errorf("record seeded player: %w", err)
	}
	eventID, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("read seeded player event ID: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO players (id, name, name_key, alias, created_event_id)
		VALUES (?, ?, ?, ?, ?)`, id, name, key, alias, eventID); err != nil {
		return fmt.Errorf("insert seeded player: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO player_events (event_id, player_id, kind)
		VALUES (?, ?, 'player_created')`, eventID, id); err != nil {
		return fmt.Errorf("record seeded player history: %w", err)
	}
	return nil
}

func newPlayerID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("generate seeded player ID: %w", err)
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(bytes[:])
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}

// Tournament reads current durable state and validates the persisted configuration.
func (s *Store) Tournament(ctx context.Context) (TournamentRecord, error) {
	if err := s.checkOpen(); err != nil {
		return TournamentRecord{}, err
	}
	var record TournamentRecord
	var configJSON string
	var created, updated string
	var started, ended sql.NullString
	var clockNS, realNS, remainingNS int64
	var segment int
	var running, registrationClosed, segmentFinished, pendingIndex int
	var holdReason sql.NullString
	var resultCompleted sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT name, status, configuration_json, created_at, updated_at,
		started_at, ended_at, clock_elapsed_ns, real_elapsed_ns, segment_index, remaining_ns,
		clock_running, registration_closed, segment_finished, hold_reason, pending_segment_index, result_completed FROM tournament WHERE id = 1`).Scan(
		&record.Name, &record.Status, &configJSON, &created, &updated, &started, &ended,
		&clockNS, &realNS, &segment, &remainingNS, &running, &registrationClosed, &segmentFinished, &holdReason, &pendingIndex, &resultCompleted)
	if err != nil {
		return TournamentRecord{}, err
	}
	if err := json.Unmarshal([]byte(configJSON), &record.Config); err != nil {
		return TournamentRecord{}, fmt.Errorf("decode stored tournament configuration: %w", err)
	}
	if err := record.Config.Validate(); err != nil {
		return TournamentRecord{}, fmt.Errorf("stored tournament configuration is invalid: %w", err)
	}
	if record.CreatedAt, err = parseWallTime(created); err != nil {
		return TournamentRecord{}, err
	}
	if record.UpdatedAt, err = parseWallTime(updated); err != nil {
		return TournamentRecord{}, err
	}
	if started.Valid {
		parsed, parseErr := parseWallTime(started.String)
		if parseErr != nil {
			return TournamentRecord{}, parseErr
		}
		record.StartedAt = &parsed
	}
	if ended.Valid {
		parsed, parseErr := parseWallTime(ended.String)
		if parseErr != nil {
			return TournamentRecord{}, parseErr
		}
		record.EndedAt = &parsed
	}
	record.Checkpoint = Checkpoint{
		ClockElapsed: time.Duration(clockNS), RealElapsed: time.Duration(realNS),
		SegmentIndex: segment, Remaining: time.Duration(remainingNS), Running: running != 0,
		SegmentFinished: segmentFinished != 0, PendingIndex: pendingIndex,
	}
	if holdReason.Valid {
		record.Checkpoint.HoldReason = holdReason.String
	}
	record.RegistrationClosed = registrationClosed != 0
	if resultCompleted.Valid {
		value := resultCompleted.Int64 != 0
		record.ResultCompleted = &value
	}
	return record, nil
}

func (s *Store) recoverOnOpen(ctx context.Context) error {
	record, err := s.Tournament(ctx)
	if err != nil {
		return err
	}
	if record.Status == StatusEnded {
		return nil
	}
	checkpoint := record.Checkpoint
	if record.StartedAt != nil {
		wallElapsed := time.Since(*record.StartedAt)
		if wallElapsed > checkpoint.RealElapsed {
			checkpoint.RealElapsed = wallElapsed
		}
	}
	needsPause := record.Status == StatusRunning || checkpoint.Running
	if needsPause {
		checkpoint.Running = false
		wall := time.Now()
		payload := map[string]any{
			"reason":           "application_reopened",
			"clock_elapsed_ns": int64(checkpoint.ClockElapsed),
			"remaining_ns":     int64(checkpoint.Remaining),
		}
		_, err := s.CommitEvent(ctx, EventInput{
			Kind: "clock_recovered_paused", Payload: payload, WallTime: wall,
			ClockElapsed: &checkpoint.ClockElapsed, RealElapsed: &checkpoint.RealElapsed,
		}, &checkpoint, func(tx *sql.Tx, _ int64) error {
			_, err := tx.ExecContext(ctx, `UPDATE tournament SET status = CASE WHEN status = ? THEN ? ELSE status END,
				clock_running = 0 WHERE id = 1`, StatusRunning, StatusPaused)
			return err
		})
		return err
	}
	if checkpoint.RealElapsed > record.Checkpoint.RealElapsed {
		return s.SaveCheckpoint(ctx, checkpoint, time.Now())
	}
	return nil
}

// RenameTournament changes the tournament name and audits it alongside any active clock checkpoint.
func (s *Store) RenameTournament(ctx context.Context, name string, at time.Time, checkpoint *Checkpoint) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("tournament name is required")
	}
	if len([]rune(name)) > MaxNameLength {
		return fmt.Errorf("tournament name cannot exceed %d characters", MaxNameLength)
	}
	if at.IsZero() {
		at = time.Now()
	}
	payload := map[string]string{"name": name}
	event := EventInput{Kind: "tournament_renamed", Payload: payload, WallTime: at}
	if checkpoint != nil {
		event.ClockElapsed = &checkpoint.ClockElapsed
		event.RealElapsed = &checkpoint.RealElapsed
	}
	_, err := s.CommitEvent(ctx, event, checkpoint, func(tx *sql.Tx, _ int64) error {
		var status TournamentStatus
		var started sql.NullString
		if err := tx.QueryRowContext(ctx, "SELECT status, started_at FROM tournament WHERE id = 1").Scan(&status, &started); err != nil {
			return err
		}
		if status == StatusEnded || started.Valid != (checkpoint != nil) || !started.Valid && status != StatusNotStarted {
			return ErrConfigurationLocked
		}
		_, err := tx.ExecContext(ctx, `UPDATE tournament SET name = ?, updated_at = ? WHERE id = 1`, name, FormatWallTime(at))
		return err
	})
	return err
}

// ViewTx runs a read-only transaction for consistent domain queries.
func (s *Store) ViewTx(ctx context.Context, view func(*sql.Tx) error) error {
	if err := s.checkOpen(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("begin read transaction: %w", err)
	}
	defer tx.Rollback()
	if err := view(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("finish read transaction: %w", err)
	}
	return nil
}

// CommitEvent records an audit event, domain updates, and an optional clock checkpoint atomically.
// The mutation callback runs in the same transaction and receives the new audit event ID.
func (s *Store) CommitEvent(ctx context.Context, event EventInput, checkpoint *Checkpoint, mutation func(*sql.Tx, int64) error) (int64, error) {
	if err := s.checkOpen(); err != nil {
		return 0, err
	}
	if strings.TrimSpace(event.Kind) == "" {
		return 0, fmt.Errorf("event kind is required")
	}
	if checkpoint != nil {
		if err := validateCheckpoint(*checkpoint); err != nil {
			return 0, err
		}
	}
	hasClockTime := event.ClockElapsed != nil
	hasRealTime := event.RealElapsed != nil
	if hasClockTime != hasRealTime {
		return 0, fmt.Errorf("event must record both elapsed clock time and elapsed real time, or neither")
	}
	if hasClockTime && checkpoint == nil {
		return 0, fmt.Errorf("event with elapsed time requires an atomic clock checkpoint")
	}
	if hasClockTime && (*event.ClockElapsed > checkpoint.ClockElapsed || *event.RealElapsed > checkpoint.RealElapsed) {
		return 0, fmt.Errorf("event elapsed time cannot exceed its clock checkpoint")
	}
	payload, err := encodePayload(event.Payload)
	if err != nil {
		return 0, fmt.Errorf("encode %s event payload: %w", event.Kind, err)
	}
	wall := event.WallTime
	if wall.IsZero() {
		wall = time.Now()
	}
	wallString := FormatWallTime(wall)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin audit transaction: %w", err)
	}
	defer tx.Rollback()
	if event.ClockElapsed != nil {
		var oldClock, oldReal int64
		if err := tx.QueryRowContext(ctx, "SELECT clock_elapsed_ns, real_elapsed_ns FROM tournament WHERE id = 1").Scan(&oldClock, &oldReal); err != nil {
			return 0, fmt.Errorf("read clock checkpoint before event: %w", err)
		}
		if int64(*event.ClockElapsed) < oldClock || int64(*event.RealElapsed) < oldReal {
			return 0, ErrCheckpointRegression
		}
	}
	eventID, err := insertEvent(ctx, tx, event, payload, wallString)
	if err != nil {
		return 0, err
	}
	if mutation != nil {
		if err := mutation(tx, eventID); err != nil {
			return 0, fmt.Errorf("apply %s event: %w", event.Kind, err)
		}
	}
	if checkpoint != nil {
		if err := updateCheckpointTx(ctx, tx, *checkpoint, wallString); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit %s event: %w", event.Kind, err)
	}
	return eventID, nil
}

func insertEvent(ctx context.Context, tx *sql.Tx, event EventInput, payload []byte, wallString string) (int64, error) {
	var clockElapsed, realElapsed any
	if event.ClockElapsed != nil {
		if *event.ClockElapsed < 0 {
			return 0, fmt.Errorf("event clock elapsed time cannot be negative")
		}
		clockElapsed = int64(*event.ClockElapsed)
	}
	if event.RealElapsed != nil {
		if *event.RealElapsed < 0 {
			return 0, fmt.Errorf("event real elapsed time cannot be negative")
		}
		realElapsed = int64(*event.RealElapsed)
	}
	var parent, voids any
	if event.ParentEventID != nil {
		if *event.ParentEventID <= 0 {
			return 0, fmt.Errorf("parent event ID must be positive")
		}
		parent = *event.ParentEventID
	}
	if event.VoidEventID != nil {
		if *event.VoidEventID <= 0 {
			return 0, fmt.Errorf("voided event ID must be positive")
		}
		voids = *event.VoidEventID
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO audit_events
		(kind, wall_timestamp, clock_elapsed_ns, real_elapsed_ns, payload_json, explanation, parent_event_id, voids_event_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, event.Kind, wallString, clockElapsed, realElapsed, string(payload), nullableString(event.Explanation), parent, voids)
	if err != nil {
		return 0, fmt.Errorf("insert %s audit event: %w", event.Kind, err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read %s audit event ID: %w", event.Kind, err)
	}
	return id, nil
}

func encodePayload(value any) ([]byte, error) {
	if value == nil {
		return []byte("{}"), nil
	}
	if raw, ok := value.(json.RawMessage); ok {
		if !json.Valid(raw) {
			return nil, fmt.Errorf("payload is not valid JSON")
		}
		return append([]byte(nil), raw...), nil
	}
	return json.Marshal(value)
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// SaveCheckpoint persists the clock state without adding a separate audit event.
func (s *Store) SaveCheckpoint(ctx context.Context, checkpoint Checkpoint, at time.Time) error {
	if err := s.checkOpen(); err != nil {
		return err
	}
	if err := validateCheckpoint(checkpoint); err != nil {
		return err
	}
	if at.IsZero() {
		at = time.Now()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin clock checkpoint: %w", err)
	}
	defer tx.Rollback()
	if err := updateCheckpointTx(ctx, tx, checkpoint, FormatWallTime(at)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit clock checkpoint: %w", err)
	}
	return nil
}

func validateCheckpoint(checkpoint Checkpoint) error {
	if checkpoint.ClockElapsed < 0 || checkpoint.RealElapsed < 0 || checkpoint.Remaining < 0 || checkpoint.SegmentIndex < 0 || checkpoint.PendingIndex < 0 {
		return fmt.Errorf("clock checkpoint values cannot be negative")
	}
	if checkpoint.SegmentFinished && checkpoint.Remaining != 0 {
		return fmt.Errorf("finished schedule segment must have zero remaining time")
	}
	if checkpoint.PendingIndex > 0 && (!checkpoint.SegmentFinished || checkpoint.HoldReason == "") {
		return fmt.Errorf("pending schedule segment requires a finished segment and hold reason")
	}
	if !checkpoint.SegmentFinished && (checkpoint.PendingIndex > 0 || checkpoint.HoldReason != "") {
		return fmt.Errorf("hold metadata requires a finished schedule segment")
	}
	if checkpoint.Running && checkpoint.SegmentIndex == 0 {
		return fmt.Errorf("running clock checkpoint requires a selected schedule segment")
	}
	return nil
}

func updateCheckpointTx(ctx context.Context, tx *sql.Tx, checkpoint Checkpoint, wallString string) error {
	var oldClock, oldReal int64
	if err := tx.QueryRowContext(ctx, "SELECT clock_elapsed_ns, real_elapsed_ns FROM tournament WHERE id = 1").Scan(&oldClock, &oldReal); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotTournamentDatabase
		}
		return fmt.Errorf("read prior clock checkpoint: %w", err)
	}
	if int64(checkpoint.ClockElapsed) < oldClock || int64(checkpoint.RealElapsed) < oldReal {
		return ErrCheckpointRegression
	}
	_, err := tx.ExecContext(ctx, `UPDATE tournament SET clock_elapsed_ns = ?, real_elapsed_ns = ?,
		segment_index = ?, remaining_ns = ?, clock_running = ?, segment_finished = ?, hold_reason = ?,
		pending_segment_index = ?, updated_at = ? WHERE id = 1`,
		int64(checkpoint.ClockElapsed), int64(checkpoint.RealElapsed), checkpoint.SegmentIndex,
		int64(checkpoint.Remaining), boolInt(checkpoint.Running), boolInt(checkpoint.SegmentFinished),
		nullableString(checkpoint.HoldReason), checkpoint.PendingIndex, wallString)
	if err != nil {
		return fmt.Errorf("update clock checkpoint: %w", err)
	}
	return nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// Events returns the audit log in commit order.
func (s *Store) Events(ctx context.Context) ([]EventRecord, error) {
	if err := s.checkOpen(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, kind, wall_timestamp, clock_elapsed_ns, real_elapsed_ns,
		payload_json, COALESCE(explanation, ''), parent_event_id, voids_event_id FROM audit_events ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query audit events: %w", err)
	}
	defer rows.Close()
	var events []EventRecord
	for rows.Next() {
		var event EventRecord
		var wall string
		var clockNS, realNS sql.NullInt64
		var parent, voids sql.NullInt64
		var payload string
		if err := rows.Scan(&event.ID, &event.Kind, &wall, &clockNS, &realNS, &payload, &event.Explanation, &parent, &voids); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		if event.WallTime, err = parseWallTime(wall); err != nil {
			return nil, err
		}
		event.Payload = json.RawMessage(payload)
		if clockNS.Valid {
			value := time.Duration(clockNS.Int64)
			event.ClockElapsed = &value
		}
		if realNS.Valid {
			value := time.Duration(realNS.Int64)
			event.RealElapsed = &value
		}
		if parent.Valid {
			value := parent.Int64
			event.ParentEventID = &value
		}
		if voids.Valid {
			value := voids.Int64
			event.VoidEventID = &value
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit events: %w", err)
	}
	return events, nil
}

// Path returns the canonical database file path.
func (s *Store) Path() string { return s.path }

func (s *Store) checkOpen() error {
	if s == nil || s.db == nil || s.lock == nil {
		return ErrStoreClosed
	}
	return nil
}

// Close releases the database and app lock. The small .lock sidecar is intentionally retained:
// deleting it could let two processes lock different inodes for the same tournament.
func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	var errs []error
	if s.db != nil {
		if err := s.db.Close(); err != nil {
			errs = append(errs, err)
		}
		s.db = nil
	}
	if s.lock != nil {
		if err := s.lock.Close(); err != nil {
			errs = append(errs, err)
		}
		s.lock = nil
	}
	return errors.Join(errs...)
}

// FormatWallTime stores a local wall-clock timestamp with its UTC offset.
func FormatWallTime(at time.Time) string { return at.In(time.Local).Format(time.RFC3339Nano) }

func parseWallTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid stored wall timestamp %q: %w", value, err)
	}
	return parsed, nil
}
