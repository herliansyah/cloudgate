package storage

import (
	"context"
	"fmt"
	"io"
)

const transferBufferSize = 32 * 1024 // 32KB chunk buffer for low-overhead streaming

// CopyFile streams a file between drivers using in-memory piping without touching local disk.
func CopyFile(ctx context.Context, srcDriver Driver, srcPath string, dstDriver Driver, dstPath string) error {
	rc, info, err := srcDriver.Get(ctx, srcPath)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer rc.Close()

	pr, pw := io.Pipe()
	errChan := make(chan error, 1)

	// Stream concurrently
	go func() {
		buf := make([]byte, transferBufferSize)
		_, copyErr := io.CopyBuffer(pw, rc, buf)
		_ = pw.CloseWithError(copyErr)
		errChan <- copyErr
	}()

	putErr := dstDriver.Put(ctx, dstPath, pr, info.Size)
	pipeErr := <-errChan

	if putErr != nil {
		return fmt.Errorf("failed to put data into target driver: %w", putErr)
	}
	if pipeErr != nil {
		return fmt.Errorf("streaming pipe error: %w", pipeErr)
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
	_, targetInfo, err := dstDriver.Get(ctx, dstPath)
	if err != nil {
		return fmt.Errorf("target verification failed after copy: %w", err)
	}

	_, srcInfo, err := srcDriver.Get(ctx, srcPath)
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
