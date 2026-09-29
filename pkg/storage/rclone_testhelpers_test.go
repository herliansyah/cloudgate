package storage

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"github.com/rclone/rclone/fs"
)

func testEd25519Key(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return priv
}

type usageT struct{ total, used, free *int64 }

func (u usageT) toUsage() fs.Usage { return fs.Usage{Total: u.total, Used: u.used, Free: u.free} }
