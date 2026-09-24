package db

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type RemoteAccount struct {
	ID          string    `json:"id"`
	Provider    string    `json:"provider"`
	Name        string    `json:"name"`
	RootFolder  string    `json:"root_folder"`
	Status      string    `json:"status"` // "connected", "disconnected", "error"
	QuotaTotal  int64     `json:"quota_total"`
	QuotaUsed   int64     `json:"quota_used"`
	Credentials string    `json:"credentials,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
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
	`
	if _, err := d.conn.Exec(schema); err != nil {
		return err
	}
	// Best-effort column addition for existing databases
	_, _ = d.conn.Exec(`ALTER TABLE accounts ADD COLUMN credentials TEXT DEFAULT ''`)
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
	_, err := d.conn.Exec(`
		INSERT INTO accounts (id, provider, name, root_folder, status, quota_total, quota_used, credentials, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			provider=excluded.provider,
			name=excluded.name,
			root_folder=excluded.root_folder,
			status=excluded.status,
			quota_total=excluded.quota_total,
			quota_used=excluded.quota_used,
			credentials=excluded.credentials,
			updated_at=excluded.updated_at
	`, acc.ID, acc.Provider, acc.Name, acc.RootFolder, acc.Status, acc.QuotaTotal, acc.QuotaUsed, acc.Credentials, time.Now().UTC())
	return err
}

// GetAccounts retrieves all saved remote accounts.
func (d *DB) GetAccounts() ([]RemoteAccount, error) {
	rows, err := d.conn.Query(`
		SELECT id, provider, name, root_folder, status, quota_total, quota_used, COALESCE(credentials, ''), updated_at
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
		if err := rows.Scan(&acc.ID, &acc.Provider, &acc.Name, &acc.RootFolder, &acc.Status, &acc.QuotaTotal, &acc.QuotaUsed, &acc.Credentials, &acc.UpdatedAt); err != nil {
			return nil, err
		}
		accounts = append(accounts, acc)
	}
	return accounts, rows.Err()
}

// GetAccount retrieves a single remote account by ID.
func (d *DB) GetAccount(id string) (*RemoteAccount, error) {
	var acc RemoteAccount
	err := d.conn.QueryRow(`
		SELECT id, provider, name, root_folder, status, quota_total, quota_used, COALESCE(credentials, ''), updated_at
		FROM accounts
		WHERE id = ?
	`, id).Scan(&acc.ID, &acc.Provider, &acc.Name, &acc.RootFolder, &acc.Status, &acc.QuotaTotal, &acc.QuotaUsed, &acc.Credentials, &acc.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &acc, nil
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
