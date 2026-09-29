package storage

import (
	"context"
	"fmt"
)

// CopyFile streams a file between drivers without touching local disk.
func CopyFile(ctx context.Context, srcDriver Driver, srcPath string, dstDriver Driver, dstPath string) error {
	rc, info, err := srcDriver.Get(ctx, srcPath)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer rc.Close()

	if err := dstDriver.Put(ctx, dstPath, rc, info.Size); err != nil {
		return fmt.Errorf("failed to put data into target driver: %w", err)
	}
	return nil
}

// MoveFile moves a file across drivers.
// If both source and destination are on the same driver, native driver Move is used.
// If cross-driver, data is streamed in-memory and source is deleted ONLY after target write verifies.
func MoveFile(ctx context.Context, srcDriver Driver, srcPath string, dstDriver Driver, dstPath string) error {
	if srcDriver.ID() == dstDriver.ID() {
		return srcDriver.Move(ctx, srcPath, dstPath)
	}

	// 1. Cross-account streaming copy
	if err := CopyFile(ctx, srcDriver, srcPath, dstDriver, dstPath); err != nil {
		return fmt.Errorf("cross-account copy failed during move: %w", err)
	}

	// 2. Safe-move verification: ensure target exists and size matches
	targetInfo, err := Stat(ctx, dstDriver, dstPath)
	if err != nil {
		return fmt.Errorf("target verification failed after copy: %w", err)
	}

	srcInfo, err := Stat(ctx, srcDriver, srcPath)
	if err != nil {
		return fmt.Errorf("source re-check failed: %w", err)
	}

	if targetInfo.Size != srcInfo.Size {
		// Size mismatch: abort and remove corrupted target, leave source intact
		_ = dstDriver.Delete(ctx, dstPath)
		return fmt.Errorf("size mismatch: source had %d bytes, target received %d bytes", srcInfo.Size, targetInfo.Size)
	}

	// 3. Source deletion only after complete verification
	if err := srcDriver.Delete(ctx, srcPath); err != nil {
		return fmt.Errorf("target copied successfully, but failed to delete original source file: %w", err)
	}

	return nil
}
