package storage

import (
	"context"
	"fmt"
	"path"
	"strings"
)

// cleanRelPath computes the relative subpath of full relative to base, using forward slashes.
func cleanRelPath(base, full string) string {
	b := path.Clean("/" + base)
	f := path.Clean("/" + full)
	if b == "/" {
		return strings.TrimPrefix(f, "/")
	}
	if f == b {
		return ""
	}
	prefix := b + "/"
	if strings.HasPrefix(f, prefix) {
		return strings.TrimPrefix(f, prefix)
	}
	// ponytail: If f is outside base, return its base name to avoid false prefix match
	return path.Base(f)
}

// WalkDriver recursively traverses a folder on a Driver and collects all files and subdirectories.
func WalkDriver(ctx context.Context, drv Driver, rootPath string) (files []FileInfo, dirs []string, err error) {
	var walk func(currentDir string) error
	walk = func(currentDir string) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		items, err := drv.List(ctx, currentDir)
		if err != nil {
			return err
		}

		for _, item := range items {
			// Skip internal trash directory
			if strings.HasPrefix(item.Path, RemoteTrashDir) {
				continue
			}
			if item.IsDir {
				dirs = append(dirs, item.Path)
				if err := walk(item.Path); err != nil {
					return err
				}
			} else {
				files = append(files, item)
			}
		}
		return nil
	}

	if err := walk(rootPath); err != nil {
		return nil, nil, err
	}
	return files, dirs, nil
}

// TransferFolder recursively copies or moves an entire directory tree across drivers.
func TransferFolder(
	ctx context.Context,
	srcDriver Driver,
	srcPath string,
	dstDriver Driver,
	dstPath string,
	isMove bool,
	onProgress func(processedBytes, totalBytes int64, itemsProcessed, totalItems int),
) (int64, int, []string, error) {
	files, dirs, err := WalkDriver(ctx, srcDriver, srcPath)
	if err != nil {
		return 0, 0, nil, fmt.Errorf("failed to scan source directory %s: %w", srcPath, err)
	}

	var totalBytes int64
	for _, f := range files {
		totalBytes += f.Size
	}
	totalItems := len(files)

	if onProgress != nil {
		onProgress(0, totalBytes, 0, totalItems)
	}

	// Pre-create destination directories
	for _, dir := range dirs {
		select {
		case <-ctx.Done():
			return 0, 0, nil, ctx.Err()
		default:
		}
		rel := cleanRelPath(srcPath, dir)
		if rel != "" {
			targetDir := path.Join(dstPath, rel)
			_ = dstDriver.Mkdir(ctx, targetDir)
		}
	}

	var processedBytes int64
	var itemsProcessed int
	var errorLog []string

	for _, file := range files {
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

		var opErr error
		if isMove {
			opErr = MoveFile(ctx, srcDriver, file.Path, dstDriver, targetFilePath)
		} else {
			opErr = CopyFile(ctx, srcDriver, file.Path, dstDriver, targetFilePath)
		}

		if opErr != nil {
			errorLog = append(errorLog, fmt.Sprintf("%s: %v", file.Path, opErr))
		} else {
			processedBytes += file.Size
			itemsProcessed++
		}

		if onProgress != nil {
			onProgress(processedBytes, totalBytes, itemsProcessed, totalItems)
		}
	}

	// If moving and no errors occurred, clean up source empty directories from deepest to root
	if isMove && len(errorLog) == 0 {
		for i := len(dirs) - 1; i >= 0; i-- {
			_ = srcDriver.Delete(ctx, dirs[i])
		}
		_ = srcDriver.Delete(ctx, srcPath)
	}

	return processedBytes, itemsProcessed, errorLog, nil
}
