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

func TestColorDetectionAndStartupBanner(t *testing.T) {
	// Test NO_COLOR environment check
	t.Setenv("NO_COLOR", "1")
	if isColorSupported() {
		t.Errorf("expected isColorSupported() to be false when NO_COLOR is set")
	}

	// Test TERM=dumb
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "dumb")
	if isColorSupported() {
		t.Errorf("expected isColorSupported() to be false when TERM=dumb")
	}

	// Test printStartupBanner execution without panicking
	printStartupBanner("0.0.0.0", 5210, "http://127.0.0.1:5210", []string{"192.168.1.50"}, "/tmp/cg_test")
	printVersion()
	printHelp()
}

