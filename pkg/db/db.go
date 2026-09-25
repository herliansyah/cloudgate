package db

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type RemoteAccount struct {
	ID          string     `json:"id"`
	Provider    string     `json:"provider"`
	Name        string     `json:"name"`
	RootFolder  string     `json:"root_folder"`
	Status      string     `json:"status"` // "connected", "disconnected", "error"
	QuotaTotal  int64      `json:"quota_total"`
	QuotaUsed   int64      `json:"quota_used"`
	Credentials string     `json:"credentials,omitempty"`
	Enabled     bool       `json:"enabled"`
	Email       string     `json:"email,omitempty"`
	LastSyncAt  *time.Time `json:"last_sync_at,omitempty"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type StarredRecord struct {
	ID        string    `json:"id"`
	AccountID string    `json:"account_id"`
	Path      string    `json:"path"`
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	IsDir     bool      `json:"is_dir"`
	CreatedAt time.Time `json:"created_at"`
}


type TrashRecord struct {
	ID              string    `json:"id"`
	AccountID       string    `json:"account_id"`
	OriginalPath    string    `json:"original_path"`
	RemoteTrashPath string    `json:"remote_trash_path"`
	FileName        string    `json:"file_name"`
	Size            int64     `json:"size"`
	DeletedAt       time.Time `json:"deleted_at"`
}

type AuditEvent struct {
	ID         int64     `json:"id"`
	Action     string    `json:"action"` // "upload", "download", "copy", "move", "trash", "restore", "connect"
	Target     string    `json:"target"`
	AccountID  string    `json:"account_id"`
	Details    string    `json:"details"`
	Status     string    `json:"status"` // "success", "failed"
	DurationMs int64     `json:"duration_ms"`
	CreatedAt  time.Time `json:"created_at"`
}

type DB struct {
	conn *sql.DB
}

// Open initializes SQLite connection and runs initial schema migrations.
func Open(dataDir string) (*DB, error) {
	dbPath := filepath.Join(dataDir, "cloudgate.db")
	conn, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	d := &DB{conn: conn}
	if err := d.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return d, nil
}

// Close closes the database connection.
func (d *DB) Close() error {
	return d.conn.Close()
}

func (d *DB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS accounts (
		id TEXT PRIMARY KEY,
		provider TEXT NOT NULL,
		name TEXT NOT NULL,
		root_folder TEXT NOT NULL,
		status TEXT NOT NULL,
		quota_total INTEGER NOT NULL DEFAULT 0,
		quota_used INTEGER NOT NULL DEFAULT 0,
		updated_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS pools (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		account_ids TEXT NOT NULL,
		strategy TEXT NOT NULL,
		created_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS trash_records (
		id TEXT PRIMARY KEY,
		account_id TEXT NOT NULL,
		original_path TEXT NOT NULL,
		remote_trash_path TEXT NOT NULL,
		file_name TEXT NOT NULL,
		size INTEGER NOT NULL,
		deleted_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS audit_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		action TEXT NOT NULL,
		target TEXT NOT NULL,
		account_id TEXT NOT NULL,
		details TEXT NOT NULL,
		status TEXT NOT NULL,
		duration_ms INTEGER NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TRIGGER IF NOT EXISTS limit_audit_events
	AFTER INSERT ON audit_events
	BEGIN
		DELETE FROM audit_events
		WHERE id NOT IN (
			SELECT id FROM audit_events ORDER BY id DESC LIMIT 100
		);
	END;

	CREATE TABLE IF NOT EXISTS starred_files (
		id TEXT PRIMARY KEY,
		account_id TEXT NOT NULL,
		path TEXT NOT NULL,
		name TEXT NOT NULL,
		size INTEGER NOT NULL DEFAULT 0,
		is_dir INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS gateway_auth (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		password_hash TEXT NOT NULL,
		updated_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS gateway_sessions (
		token TEXT PRIMARY KEY,
		created_at DATETIME NOT NULL,
		expires_at DATETIME NOT NULL
	);
	`
	if _, err := d.conn.Exec(schema); err != nil {
		return err
	}
	// Best-effort column addition for existing databases
	_, _ = d.conn.Exec(`ALTER TABLE accounts ADD COLUMN credentials TEXT DEFAULT ''`)
	_, _ = d.conn.Exec(`ALTER TABLE accounts ADD COLUMN enabled INTEGER DEFAULT 1`)
	_, _ = d.conn.Exec(`ALTER TABLE accounts ADD COLUMN email TEXT DEFAULT ''`)
	_, _ = d.conn.Exec(`ALTER TABLE accounts ADD COLUMN last_sync_at DATETIME`)
	return nil
}

// RecordAudit inserts an audit event and relies on the trigger to enforce the rolling 100-event limit.
func (d *DB) RecordAudit(action, target, accountID, details, status string, durationMs int64) error {
	_, err := d.conn.Exec(`
		INSERT INTO audit_events (action, target, account_id, details, status, duration_ms, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, action, target, accountID, details, status, durationMs, time.Now().UTC())
	return err
}

// GetRecentAuditEvents returns the stored audit events (up to 100), ordered newest first.
func (d *DB) GetRecentAuditEvents() ([]AuditEvent, error) {
	rows, err := d.conn.Query(`
		SELECT id, action, target, account_id, details, status, duration_ms, created_at
		FROM audit_events
		ORDER BY id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]AuditEvent, 0)
	for rows.Next() {
		var ev AuditEvent
		if err := rows.Scan(&ev.ID, &ev.Action, &ev.Target, &ev.AccountID, &ev.Details, &ev.Status, &ev.DurationMs, &ev.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, ev)
	}
	return events, rows.Err()
}

// CountAuditEvents returns the current total number of audit events.
func (d *DB) CountAuditEvents() (int, error) {
	var count int
	err := d.conn.QueryRow(`SELECT COUNT(*) FROM audit_events`).Scan(&count)
	return count, err
}

// SaveAccount inserts or updates a RemoteAccount.
func (d *DB) SaveAccount(acc RemoteAccount) error {
	enabledInt := 1
	if !acc.Enabled && acc.Status == "disabled" {
		enabledInt = 0
	}
	_, err := d.conn.Exec(`
		INSERT INTO accounts (id, provider, name, root_folder, status, quota_total, quota_used, credentials, enabled, email, last_sync_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			provider=excluded.provider,
			name=excluded.name,
			root_folder=excluded.root_folder,
			status=excluded.status,
			quota_total=excluded.quota_total,
			quota_used=excluded.quota_used,
			credentials=excluded.credentials,
			enabled=COALESCE(excluded.enabled, accounts.enabled, 1),
			email=COALESCE(excluded.email, accounts.email, ''),
			last_sync_at=COALESCE(excluded.last_sync_at, accounts.last_sync_at),
			updated_at=excluded.updated_at
	`, acc.ID, acc.Provider, acc.Name, acc.RootFolder, acc.Status, acc.QuotaTotal, acc.QuotaUsed, acc.Credentials, enabledInt, acc.Email, acc.LastSyncAt, time.Now().UTC())
	return err
}

// ToggleAccount toggles the active/paused integration state of an account.
func (d *DB) ToggleAccount(id string, enabled bool) error {
	enabledInt := 0
	status := "disabled"
	if enabled {
		enabledInt = 1
		status = "connected"
	}
	_, err := d.conn.Exec(`UPDATE accounts SET enabled = ?, status = ?, updated_at = ? WHERE id = ?`, enabledInt, status, time.Now().UTC(), id)
	return err
}

// UpdateAccountLastSync records the timestamp of a successful sync operation.
func (d *DB) UpdateAccountLastSync(id string, t time.Time) error {
	_, err := d.conn.Exec(`UPDATE accounts SET last_sync_at = ?, updated_at = ? WHERE id = ?`, t.UTC(), time.Now().UTC(), id)
	return err
}

// GetAccounts retrieves all saved remote accounts.
func (d *DB) GetAccounts() ([]RemoteAccount, error) {
	rows, err := d.conn.Query(`
		SELECT id, provider, name, root_folder, status, quota_total, quota_used, COALESCE(credentials, ''), COALESCE(enabled, 1), COALESCE(email, ''), last_sync_at, updated_at
		FROM accounts
		ORDER BY name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	accounts := make([]RemoteAccount, 0)
	for rows.Next() {
		var acc RemoteAccount
		var enabledInt int
		var email sql.NullString
		var lastSync sql.NullTime
		if err := rows.Scan(&acc.ID, &acc.Provider, &acc.Name, &acc.RootFolder, &acc.Status, &acc.QuotaTotal, &acc.QuotaUsed, &acc.Credentials, &enabledInt, &email, &lastSync, &acc.UpdatedAt); err != nil {
			return nil, err
		}
		acc.Enabled = (enabledInt == 1)
		acc.Email = email.String
		if lastSync.Valid {
			acc.LastSyncAt = &lastSync.Time
		}
		accounts = append(accounts, acc)
	}
	return accounts, rows.Err()
}

// GetAccount retrieves a single remote account by ID.
func (d *DB) GetAccount(id string) (*RemoteAccount, error) {
	var acc RemoteAccount
	var enabledInt int
	var email sql.NullString
	var lastSync sql.NullTime
	err := d.conn.QueryRow(`
		SELECT id, provider, name, root_folder, status, quota_total, quota_used, COALESCE(credentials, ''), COALESCE(enabled, 1), COALESCE(email, ''), last_sync_at, updated_at
		FROM accounts
		WHERE id = ?
	`, id).Scan(&acc.ID, &acc.Provider, &acc.Name, &acc.RootFolder, &acc.Status, &acc.QuotaTotal, &acc.QuotaUsed, &acc.Credentials, &enabledInt, &email, &lastSync, &acc.UpdatedAt)
	if err != nil {
		return nil, err
	}
	acc.Enabled = (enabledInt == 1)
	acc.Email = email.String
	if lastSync.Valid {
		acc.LastSyncAt = &lastSync.Time
	}
	return &acc, nil
}

// GetStarredFiles returns all bookmarked files.
func (d *DB) GetStarredFiles() ([]StarredRecord, error) {
	rows, err := d.conn.Query(`
		SELECT id, account_id, path, name, size, is_dir, created_at
		FROM starred_files
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]StarredRecord, 0)
	for rows.Next() {
		var rec StarredRecord
		var isDirInt int
		if err := rows.Scan(&rec.ID, &rec.AccountID, &rec.Path, &rec.Name, &rec.Size, &isDirInt, &rec.CreatedAt); err != nil {
			return nil, err
		}
		rec.IsDir = (isDirInt == 1)
		list = append(list, rec)
	}
	return list, rows.Err()
}

// AddStarredFile bookmarks a file.
func (d *DB) AddStarredFile(rec StarredRecord) error {
	isDirInt := 0
	if rec.IsDir {
		isDirInt = 1
	}
	_, err := d.conn.Exec(`
		INSERT INTO starred_files (id, account_id, path, name, size, is_dir, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name,
			size=excluded.size,
			is_dir=excluded.is_dir
	`, rec.ID, rec.AccountID, rec.Path, rec.Name, rec.Size, isDirInt, time.Now().UTC())
	return err
}

// RemoveStarredFile unbookmarks a file.
func (d *DB) RemoveStarredFile(idOrAccountID, filePath string) error {
	if filePath == "" {
		_, err := d.conn.Exec(`DELETE FROM starred_files WHERE id = ?`, idOrAccountID)
		return err
	}
	_, err := d.conn.Exec(`DELETE FROM starred_files WHERE account_id = ? AND path = ?`, idOrAccountID, filePath)
	return err
}


// DeleteAccount permanently deletes an account from the database.
func (d *DB) DeleteAccount(id string) error {
	_, err := d.conn.Exec(`DELETE FROM accounts WHERE id = ?`, id)
	return err
}

// AddTrashRecord inserts a soft-deleted file mapping.
func (d *DB) AddTrashRecord(rec TrashRecord) error {
	_, err := d.conn.Exec(`
		INSERT INTO trash_records (id, account_id, original_path, remote_trash_path, file_name, size, deleted_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, rec.ID, rec.AccountID, rec.OriginalPath, rec.RemoteTrashPath, rec.FileName, rec.Size, rec.DeletedAt)
	return err
}

// GetTrashRecords lists all trash records.
func (d *DB) GetTrashRecords() ([]TrashRecord, error) {
	rows, err := d.conn.Query(`
		SELECT id, account_id, original_path, remote_trash_path, file_name, size, deleted_at
		FROM trash_records
		ORDER BY deleted_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]TrashRecord, 0)
	for rows.Next() {
		var rec TrashRecord
		if err := rows.Scan(&rec.ID, &rec.AccountID, &rec.OriginalPath, &rec.RemoteTrashPath, &rec.FileName, &rec.Size, &rec.DeletedAt); err != nil {
			return nil, err
		}
		list = append(list, rec)
	}
	return list, rows.Err()
}

// DeleteTrashRecord removes a record from the trash table upon permanent purge or restore.
func (d *DB) DeleteTrashRecord(id string) error {
	_, err := d.conn.Exec(`DELETE FROM trash_records WHERE id = ?`, id)
	return err
}

// SetMasterPassword sets or updates the master password hash.
func (d *DB) SetMasterPassword(hash string) error {
	_, err := d.conn.Exec(`
		INSERT INTO gateway_auth (id, password_hash, updated_at)
		VALUES (1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET password_hash = excluded.password_hash, updated_at = excluded.updated_at
	`, hash, time.Now().UTC())
	return err
}

// GetMasterPasswordHash returns the stored master password hash, or empty string if not configured.
func (d *DB) GetMasterPasswordHash() (string, error) {
	var hash string
	err := d.conn.QueryRow(`SELECT password_hash FROM gateway_auth WHERE id = 1`).Scan(&hash)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return hash, err
}

// ClearMasterPassword deletes the master password and wipes all active sessions.
func (d *DB) ClearMasterPassword() error {
	tx, err := d.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM gateway_auth WHERE id = 1`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM gateway_sessions`); err != nil {
		return err
	}
	return tx.Commit()
}

// HasMasterPassword returns true if a master password has been configured.
func (d *DB) HasMasterPassword() (bool, error) {
	hash, err := d.GetMasterPasswordHash()
	if err != nil {
		return false, err
	}
	return hash != "", nil
}

// CreateGatewaySession records an active session token with an expiration time.
func (d *DB) CreateGatewaySession(token string, expiresAt time.Time) error {
	_, err := d.conn.Exec(`
		INSERT INTO gateway_sessions (token, created_at, expires_at)
		VALUES (?, ?, ?)
	`, token, time.Now().UTC(), expiresAt.UTC())
	return err
}

// ValidateGatewaySession checks if a session token is valid and not expired.
func (d *DB) ValidateGatewaySession(token string) (bool, error) {
	if token == "" {
		return false, nil
	}
	var expiresAt time.Time
	err := d.conn.QueryRow(`
		SELECT expires_at FROM gateway_sessions WHERE token = ?
	`, token).Scan(&expiresAt)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if time.Now().UTC().After(expiresAt) {
		_, _ = d.conn.Exec(`DELETE FROM gateway_sessions WHERE token = ?`, token)
		return false, nil
	}
	return true, nil
}

// DeleteGatewaySession removes an active session token.
func (d *DB) DeleteGatewaySession(token string) error {
	_, err := d.conn.Exec(`DELETE FROM gateway_sessions WHERE token = ?`, token)
	return err
}

// PurgeExpiredGatewaySessions deletes all expired sessions.
func (d *DB) PurgeExpiredGatewaySessions() error {
	_, err := d.conn.Exec(`DELETE FROM gateway_sessions WHERE expires_at < ?`, time.Now().UTC())
	return err
}

