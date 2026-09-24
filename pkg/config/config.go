package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const (
	AppName    = "Cloudgate"
	AppVersion = "0.1.0"
	AppAuthor  = "Herliansyah"
	AppRepo    = "https://github.com/herliansyah/cloudgate"

	DefaultPort = 5210
	LockFileName = "cloudgate.lock"
	DBFileName   = "cloudgate.db"
	VaultFileName = "vault.enc"
)

// Dir returns the active configuration directory, creating it if necessary.
func Dir() (string, error) {
	if custom := os.Getenv("CLOUDGATE_CONFIG_DIR"); custom != "" {
		if err := os.MkdirAll(custom, 0700); err != nil {
			return "", err
		}
		return custom, nil
	}

	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		userConfigDir = os.Getenv("HOME")
		if userConfigDir == "" {
			userConfigDir = "."
		}
	}

	dir := filepath.Join(userConfigDir, "cloudgate")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

// LockInfo holds information about a running instance.
type LockInfo struct {
	PID  int
	Port int
}

// AcquireInstanceLock attempts to create a single-instance lock file.
// If another instance is running, it returns the LockInfo of that instance.
func AcquireInstanceLock(port int) (*os.File, *LockInfo, error) {
	dir, err := Dir()
	if err != nil {
		return nil, nil, err
	}
	lockPath := filepath.Join(dir, LockFileName)

	// Check if existing lock file exists and is active
	if data, err := os.ReadFile(lockPath); err == nil {
		parts := strings.Split(strings.TrimSpace(string(data)), ":")
		if len(parts) == 2 {
			pid, _ := strconv.Atoi(parts[0])
			p, _ := strconv.Atoi(parts[1])
			if pid > 0 && isProcessRunning(pid) {
				return nil, &LockInfo{PID: pid, Port: p}, fmt.Errorf("instance already running (PID: %d, Port: %d)", pid, p)
			}
		}
	}

	// Create or open the lock file
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open lock file: %w", err)
	}

	// Try non-blocking flock
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, nil, fmt.Errorf("failed to acquire file lock: %w", err)
	}

	// Write PID:Port to lock file
	if err := file.Truncate(0); err == nil {
		_, _ = file.Seek(0, 0)
		_, _ = file.WriteString(fmt.Sprintf("%d:%d\n", os.Getpid(), port))
		_ = file.Sync()
	}

	return file, nil, nil
}

// ReleaseInstanceLock releases the lock and removes the lock file.
func ReleaseInstanceLock(file *os.File) {
	if file == nil {
		return
	}
	defer file.Close()
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)

	dir, err := Dir()
	if err == nil {
		_ = os.Remove(filepath.Join(dir, LockFileName))
	}
}

// isProcessRunning checks if a PID is alive on Unix/Linux.
func isProcessRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, sending signal 0 checks if process exists without killing it
	err = process.Signal(syscall.Signal(0))
	return err == nil
}
