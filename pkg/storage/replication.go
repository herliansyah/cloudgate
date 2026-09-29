package storage

import (
	"context"
	"fmt"
	"path"
)

// ReplicateFolder synchronizes files from srcPath on srcDriver to dstPath on dstDriver.
// Default mode is additive: new or changed files are copied; existing destination files are preserved.
// If mirror is true and trashManager is non-nil, orphaned destination files that no longer exist on source
// are soft-deleted into RemoteTrash (/.cloudgate_trash/). If trashManager is nil, orphaned files are deleted.
func ReplicateFolder(
	ctx context.Context,
	srcDriver Driver,
	srcPath string,
	dstDriver Driver,
	dstPath string,
	mirror bool,
	trashManager *TrashManager,
	onProgress func(processedBytes, totalBytes int64, itemsProcessed, totalItems int),
) (int64, int, []string, error) {
	// 0. Ensure destination directory exists
	if dstPath != "" && dstPath != "/" && dstPath != "." {
		_ = dstDriver.Mkdir(ctx, dstPath)
	}

	// 1. Scan source
	srcFiles, srcDirs, err := WalkDriver(ctx, srcDriver, srcPath)
	if err != nil {
		return 0, 0, nil, fmt.Errorf("failed to scan source directory %s: %w", srcPath, err)
	}

	// 2. Scan destination (best effort; if empty or doesn't exist, treat as empty)
	dstFiles, dstDirs, _ := WalkDriver(ctx, dstDriver, dstPath)

	srcMap := make(map[string]FileInfo, len(srcFiles))
	for _, f := range srcFiles {
		srcMap[cleanRelPath(srcPath, f.Path)] = f
	}

	dstMap := make(map[string]FileInfo, len(dstFiles))
	for _, f := range dstFiles {
		dstMap[cleanRelPath(dstPath, f.Path)] = f
	}

	// 3. Determine files needing copy (size difference or newer modification time)
	var toCopy []FileInfo
	var totalBytes int64
	for rel, sf := range srcMap {
		df, exists := dstMap[rel]
		needsCopy := !exists || df.Size != sf.Size
		if !needsCopy && !sf.ModTime.IsZero() && !df.ModTime.IsZero() {
			if sf.ModTime.After(df.ModTime) {
				needsCopy = true
			}
		}
		if needsCopy {
			toCopy = append(toCopy, sf)
			totalBytes += sf.Size
		}
	}
	totalItems := len(toCopy)

	if onProgress != nil {
		onProgress(0, totalBytes, 0, totalItems)
	}

	// 4. Pre-create directories in destination
	for _, dir := range srcDirs {
		select {
		case <-ctx.Done():
			return 0, 0, nil, ctx.Err()
		default:
		}
		rel := cleanRelPath(srcPath, dir)
		if rel != "" {
			_ = dstDriver.Mkdir(ctx, path.Join(dstPath, rel))
		}
	}

	// 5. Stream copy changed or new files
	var processedBytes int64
	var itemsProcessed int
	var errorLog []string

	for _, file := range toCopy {
		select {
		case <-ctx.Done():
			return processedBytes, itemsProcessed, errorLog, ctx.Err()
		default:
		}

		rel := cleanRelPath(srcPath, file.Path)
		if rel == "" {
			rel = path.Base(file.Path)
		}
		targetFilePath := path.Join(dstPath, rel)
		targetDir := path.Dir(targetFilePath)
		if targetDir != "" && targetDir != "." && targetDir != "/" {
			_ = dstDriver.Mkdir(ctx, targetDir)
		}

		if err := CopyFile(ctx, srcDriver, file.Path, dstDriver, targetFilePath); err != nil {
			errorLog = append(errorLog, fmt.Sprintf("%s: %v", file.Path, err))
		} else {
			processedBytes += file.Size
			itemsProcessed++
		}

		if onProgress != nil {
			onProgress(processedBytes, totalBytes, itemsProcessed, totalItems)
		}
	}

	// 6. If mirror enabled, soft-delete orphaned destination files to RemoteTrash (or delete directly)
	if mirror {
		for rel, df := range dstMap {
			select {
			case <-ctx.Done():
				return processedBytes, itemsProcessed, errorLog, ctx.Err()
			default:
			}
			if _, exists := srcMap[rel]; !exists {
				if trashManager != nil {
					if _, trashErr := trashManager.MoveToTrash(ctx, dstDriver, df.Path); trashErr != nil {
						errorLog = append(errorLog, fmt.Sprintf("mirror trash %s: %v", df.Path, trashErr))
					}
				} else {
					if delErr := dstDriver.Delete(ctx, df.Path); delErr != nil {
						errorLog = append(errorLog, fmt.Sprintf("mirror delete %s: %v", df.Path, delErr))
					}
				}
			}
		}

		// Clean up orphaned empty destination directories (deepest first)
		srcDirMap := make(map[string]bool, len(srcDirs))
		for _, d := range srcDirs {
			srcDirMap[cleanRelPath(srcPath, d)] = true
		}
		for i := len(dstDirs) - 1; i >= 0; i-- {
			dPath := dstDirs[i]
			rel := cleanRelPath(dstPath, dPath)
			if !srcDirMap[rel] {
				_ = dstDriver.Delete(ctx, dPath)
			}
		}
	}

	return processedBytes, itemsProcessed, errorLog, nil
}
