package storage

import (
	"context"
	"fmt"
	"path"
	"time"

	"github.com/herliansyah/cloudgate/pkg/db"
)

const RemoteTrashDir = "/.cloudgate_trash"

type TrashManager struct {
	database *db.DB
}

func NewTrashManager(database *db.DB) *TrashManager {
	return &TrashManager{database: database}
}

// MoveToTrash moves a remote file into the isolated /.cloudgate_trash directory and catalogs its restoration path in SQLite.
func (tm *TrashManager) MoveToTrash(ctx context.Context, driver Driver, originalPath string) (*db.TrashRecord, error) {
	_, info, err := driver.Get(ctx, originalPath)
	if err != nil {
		return nil, fmt.Errorf("failed to locate file for trash: %w", err)
	}

	fileName := path.Base(originalPath)
	uniqueTrashName := fmt.Sprintf("%d_%s", time.Now().UnixNano(), fileName)
	remoteTrashPath := path.Join(RemoteTrashDir, uniqueTrashName)

	if err := driver.Move(ctx, originalPath, remoteTrashPath); err != nil {
		return nil, fmt.Errorf("failed to move file to remote trash directory: %w", err)
	}

	rec := db.TrashRecord{
		ID:              fmt.Sprintf("trash_%d", time.Now().UnixNano()),
		AccountID:       driver.ID(),
		OriginalPath:    originalPath,
		RemoteTrashPath: remoteTrashPath,
		FileName:        fileName,
		Size:            info.Size,
		DeletedAt:       time.Now().UTC(),
	}

	if err := tm.database.AddTrashRecord(rec); err != nil {
		return nil, fmt.Errorf("failed to record trash metadata in database: %w", err)
	}

	return &rec, nil
}

// Restore moves the file from /.cloudgate_trash back to its original path and removes the record from SQLite.
func (tm *TrashManager) Restore(ctx context.Context, driver Driver, trashID string) error {
	records, err := tm.database.GetTrashRecords()
	if err != nil {
		return err
	}

	var targetRec *db.TrashRecord
	for _, r := range records {
		if r.ID == trashID {
			targetRec = &r
			break
		}
	}

	if targetRec == nil {
		return fmt.Errorf("trash record %s not found", trashID)
	}

	if err := driver.Move(ctx, targetRec.RemoteTrashPath, targetRec.OriginalPath); err != nil {
		return fmt.Errorf("failed to restore file on remote driver: %w", err)
	}

	if err := tm.database.DeleteTrashRecord(trashID); err != nil {
		return fmt.Errorf("failed to remove trash record from database: %w", err)
	}

	return nil
}

// EmptyTrash permanently purges all soft-deleted files for the specified driver from remote trash and the database.
func (tm *TrashManager) EmptyTrash(ctx context.Context, driver Driver) error {
	records, err := tm.database.GetTrashRecords()
	if err != nil {
		return err
	}

	for _, r := range records {
		if r.AccountID == driver.ID() {
			_ = driver.Delete(ctx, r.RemoteTrashPath)
			_ = tm.database.DeleteTrashRecord(r.ID)
		}
	}

	return nil
}
