package config

import (
	"testing"
)

func TestInstanceLock(t *testing.T) {
	// First acquire should succeed
	file, lockInfo, err := AcquireInstanceLock(5210)
	if err != nil {
		t.Fatalf("failed to acquire first instance lock: %v", err)
	}
	if lockInfo != nil {
		t.Fatalf("expected nil lockInfo on successful acquire, got %+v", lockInfo)
	}

	// Second acquire should detect instance already running
	file2, lockInfo2, err2 := AcquireInstanceLock(5210)
	if err2 == nil {
		ReleaseInstanceLock(file2)
		t.Fatalf("expected error on second acquire, got nil")
	}
	if lockInfo2 == nil || lockInfo2.Port != 5210 {
		t.Fatalf("expected lockInfo with port 5210, got %+v", lockInfo2)
	}

	// Release first lock
	ReleaseInstanceLock(file)

	// Third acquire should now succeed
	file3, _, err3 := AcquireInstanceLock(5210)
	if err3 != nil {
		t.Fatalf("failed to re-acquire instance lock after release: %v", err3)
	}
	ReleaseInstanceLock(file3)
}
