package storage

import (
	"context"
	"errors"
	"io"
	"time"
)

var (
	ErrFileNotFound         = errors.New("file not found")
	ErrInsufficientCapacity = errors.New("insufficient capacity in storage pool")
	ErrDriverNotAvailable   = errors.New("storage driver is disconnected or unavailable")
)

type FileInfo struct {
	Path        string    `json:"path"`
	Name        string    `json:"name"`
	Size        int64     `json:"size"`
	IsDir       bool      `json:"is_dir"`
	ModTime     time.Time `json:"mod_time"`
	AccountID   string    `json:"account_id,omitempty"`
	Provider    string    `json:"provider,omitempty"`
	DownloadURL string    `json:"download_url,omitempty"`
}

type QuotaInfo struct {
	Total int64 `json:"total"`
	Used  int64 `json:"used"`
	Free  int64 `json:"free"`
}

// Driver defines the core operations required from any cloud storage provider.
type Driver interface {
	ID() string
	Provider() string
	About(ctx context.Context) (QuotaInfo, error)
	List(ctx context.Context, path string) ([]FileInfo, error)
	Get(ctx context.Context, path string) (io.ReadCloser, FileInfo, error)
	Put(ctx context.Context, path string, in io.Reader, size int64) error
	Delete(ctx context.Context, path string) error
	Move(ctx context.Context, srcPath, dstPath string) error
	Mkdir(ctx context.Context, path string) error
	TestConnection(ctx context.Context) error
	GetShareLink(ctx context.Context, path string) (string, error)
}

