package storage

import (
	"context"
	"io"
	"strings"
	"testing"
)

type closeTracker struct {
	io.Reader
	closed bool
}

func (c *closeTracker) Close() error { c.closed = true; return nil }

type getOnlyDriver struct {
	*MemDriver
	rc *closeTracker
}

func (g *getOnlyDriver) Get(ctx context.Context, p string) (io.ReadCloser, FileInfo, error) {
	g.rc = &closeTracker{Reader: strings.NewReader("x")}
	return g.rc, FileInfo{Path: p, Size: 1}, nil
}

// Regression: callers that only need metadata used to leave the download
// stream open, which locks files on SMB and leaks connections.
func TestStatClosesStream(t *testing.T) {
	d := &getOnlyDriver{MemDriver: NewMemDriver("m", "mock", 10)}
	info, err := Stat(context.Background(), d, "/a")
	if err != nil || info.Size != 1 || d.rc == nil || !d.rc.closed {
		t.Fatalf("stream not closed: %+v %v", info, err)
	}
}
