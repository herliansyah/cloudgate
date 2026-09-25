package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"
	"time"
)

// MemDriver provides an in-memory Driver implementation for testing and isolated verification.
type MemDriver struct {
	mu        sync.RWMutex
	id        string
	provider  string
	quota     QuotaInfo
	files     map[string][]byte
	modTimes  map[string]time.Time
	connected bool
}

func NewMemDriver(id, provider string, totalQuota int64) *MemDriver {
	return &MemDriver{
		id:        id,
		provider:  provider,
		quota:     QuotaInfo{Total: totalQuota, Used: 0, Free: totalQuota},
		files:     make(map[string][]byte),
		modTimes:  make(map[string]time.Time),
		connected: true,
	}
}

func (m *MemDriver) ID() string       { return m.id }
func (m *MemDriver) Provider() string { return m.provider }

func (m *MemDriver) SetConnected(connected bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = connected
}

func (m *MemDriver) About(ctx context.Context) (QuotaInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.connected {
		return QuotaInfo{}, ErrDriverNotAvailable
	}
	return m.quota, nil
}

func (m *MemDriver) List(ctx context.Context, dirPath string) ([]FileInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.connected {
		return nil, ErrDriverNotAvailable
	}

	cleanDir := path.Clean("/" + dirPath)
	if cleanDir == "/" {
		cleanDir = ""
	}

	seenDirs := make(map[string]bool)
	var list []FileInfo

	for filePath, content := range m.files {
		if cleanDir != "" && !strings.HasPrefix(filePath, cleanDir+"/") {
			continue
		}

		rel := strings.TrimPrefix(filePath, cleanDir+"/")
		parts := strings.Split(rel, "/")
		if len(parts) > 1 {
			// Subdirectory
			subDir := parts[0]
			if !seenDirs[subDir] {
				seenDirs[subDir] = true
				list = append(list, FileInfo{
					Path:      path.Join(cleanDir, subDir),
					Name:      subDir,
					IsDir:     true,
					ModTime:   time.Now().UTC(),
					AccountID: m.id,
					Provider:  m.provider,
				})
			}
		} else {
			// Direct file
			list = append(list, FileInfo{
				Path:      filePath,
				Name:      parts[0],
				Size:      int64(len(content)),
				IsDir:     false,
				ModTime:   m.modTimes[filePath],
				AccountID: m.id,
				Provider:  m.provider,
			})
		}
	}
	return list, nil
}

func (m *MemDriver) Get(ctx context.Context, filePath string) (io.ReadCloser, FileInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.connected {
		return nil, FileInfo{}, ErrDriverNotAvailable
	}

	cleanPath := path.Clean("/" + filePath)
	content, ok := m.files[cleanPath]
	if !ok {
		return nil, FileInfo{}, ErrFileNotFound
	}

	info := FileInfo{
		Path:      cleanPath,
		Name:      path.Base(cleanPath),
		Size:      int64(len(content)),
		IsDir:     false,
		ModTime:   m.modTimes[cleanPath],
		AccountID: m.id,
		Provider:  m.provider,
	}
	return io.NopCloser(bytes.NewReader(content)), info, nil
}

func (m *MemDriver) Put(ctx context.Context, filePath string, in io.Reader, size int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.connected {
		return ErrDriverNotAvailable
	}

	if m.quota.Free < size {
		return ErrInsufficientCapacity
	}

	cleanPath := path.Clean("/" + filePath)
	data, err := io.ReadAll(in)
	if err != nil {
		return fmt.Errorf("failed to read input stream: %w", err)
	}

	if prev, exists := m.files[cleanPath]; exists {
		m.quota.Used -= int64(len(prev))
		m.quota.Free += int64(len(prev))
	}

	m.files[cleanPath] = data
	m.modTimes[cleanPath] = time.Now().UTC()
	m.quota.Used += int64(len(data))
	m.quota.Free = m.quota.Total - m.quota.Used

	return nil
}

func (m *MemDriver) Delete(ctx context.Context, filePath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.connected {
		return ErrDriverNotAvailable
	}

	cleanPath := path.Clean("/" + filePath)
	data, exists := m.files[cleanPath]
	if !exists {
		return ErrFileNotFound
	}

	m.quota.Used -= int64(len(data))
	m.quota.Free = m.quota.Total - m.quota.Used
	delete(m.files, cleanPath)
	delete(m.modTimes, cleanPath)
	return nil
}

func (m *MemDriver) Move(ctx context.Context, srcPath, dstPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.connected {
		return ErrDriverNotAvailable
	}

	cleanSrc := path.Clean("/" + srcPath)
	cleanDst := path.Clean("/" + dstPath)

	data, exists := m.files[cleanSrc]
	if !exists {
		return ErrFileNotFound
	}

	m.files[cleanDst] = data
	m.modTimes[cleanDst] = m.modTimes[cleanSrc]
	delete(m.files, cleanSrc)
	delete(m.modTimes, cleanSrc)
	return nil
}

func (m *MemDriver) Mkdir(ctx context.Context, dirPath string) error {
	return nil // Implicit in virtual path map
}

func (m *MemDriver) TestConnection(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.connected {
		return ErrDriverNotAvailable
	}
	return nil
}

func (m *MemDriver) GetShareLink(ctx context.Context, filePath string) (string, error) {
	return fmt.Sprintf("/api/files/download?path=%s&account_id=%s", filePath, m.id), nil
}

