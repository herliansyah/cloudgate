package db

import (
	"fmt"
	"strings"
	"time"
)

type IndexedFile struct {
	AccountID string    `json:"account_id"`
	Path      string    `json:"path"`
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	IsDir     bool      `json:"is_dir"`
	ModTime   time.Time `json:"mod_time"`
}

// EnableFTSIndex ensures the FTS5 virtual table is created.
func (d *DB) EnableFTSIndex() error {
	schema := `
	CREATE VIRTUAL TABLE IF NOT EXISTS file_index USING fts5(
		account_id,
		path,
		name,
		size,
		is_dir,
		mod_time
	);
	`
	_, err := d.conn.Exec(schema)
	return err
}

// ClearAccountIndex clears indexed files for a given account before re-indexing.
func (d *DB) ClearAccountIndex(accountID string) error {
	_, err := d.conn.Exec(`DELETE FROM file_index WHERE account_id = ?`, accountID)
	return err
}

// IndexFiles inserts a batch of files into the FTS5 index.
func (d *DB) IndexFiles(files []IndexedFile) error {
	if len(files) == 0 {
		return nil
	}

	tx, err := d.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO file_index (account_id, path, name, size, is_dir, mod_time)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, f := range files {
		isDirInt := 0
		if f.IsDir {
			isDirInt = 1
		}
		_, err := stmt.Exec(f.AccountID, f.Path, f.Name, f.Size, isDirInt, f.ModTime.Format(time.RFC3339))
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// SearchFiles queries the FTS5 index using prefix matching.
func (d *DB) SearchFiles(query string, limit int) ([]IndexedFile, error) {
	if limit <= 0 {
		limit = 50
	}

	cleanQuery := strings.TrimSpace(query)
	if cleanQuery == "" {
		return nil, nil
	}

	// Append wildcard for prefix search
	ftsQuery := fmt.Sprintf("%s*", cleanQuery)

	rows, err := d.conn.Query(`
		SELECT account_id, path, name, CAST(size AS INTEGER), CAST(is_dir AS INTEGER), mod_time
		FROM file_index
		WHERE file_index MATCH ?
		LIMIT ?
	`, ftsQuery, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []IndexedFile
	for rows.Next() {
		var f IndexedFile
		var isDirInt int
		var modTimeStr string
		if err := rows.Scan(&f.AccountID, &f.Path, &f.Name, &f.Size, &isDirInt, &modTimeStr); err != nil {
			return nil, err
		}
		f.IsDir = isDirInt == 1
		f.ModTime, _ = time.Parse(time.RFC3339, modTimeStr)
		results = append(results, f)
	}

	return results, rows.Err()
}
