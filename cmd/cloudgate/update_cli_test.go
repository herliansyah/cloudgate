package main

import (
	"strings"
	"testing"

	"github.com/herliansyah/cloudgate/pkg/updater"
)

func TestCLIChangelogAndHelp(t *testing.T) {
	changelog := updater.GetChangelog()
	if !strings.Contains(changelog, "Changelog") {
		t.Errorf("expected GetChangelog() to return changelog content, got: %s", changelog)
	}
}
