package db

import (
	"fmt"
	"os"
	"testing"
	"time"
)

func TestStorageTasks(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cloudgate-db-task-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	d, err := Open(tmpDir)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer d.Close()

	// 1. Create Task
	task := &StorageTask{
		ID:              "task-1",
		Type:            "transfer",
		SourceAccountID: "acc-1",
		SourcePath:      "/source/file.txt",
		TargetAccountID: "acc-2",
		TargetPath:      "/target/file.txt",
		Status:          "pending",
		TotalBytes:      1024,
		TotalItems:      1,
	}

	if err := d.CreateTask(task); err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	// 2. Fetch Task
	fetched, err := d.GetTask("task-1")
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}
	if fetched == nil || fetched.ID != "task-1" || fetched.Status != "pending" {
		t.Fatalf("unexpected fetched task: %+v", fetched)
	}

	// 3. GetNextPendingTask claims FIFO
	claimed, err := d.GetNextPendingTask()
	if err != nil {
		t.Fatalf("GetNextPendingTask failed: %v", err)
	}
	if claimed == nil || claimed.ID != "task-1" || claimed.Status != "running" {
		t.Fatalf("expected task-1 claimed in running state, got: %+v", claimed)
	}

	// Another claim should return nil since no more pending tasks
	claimed2, err := d.GetNextPendingTask()
	if err != nil || claimed2 != nil {
		t.Fatalf("expected nil claim, got: %+v, err: %v", claimed2, err)
	}

	// 4. Update progress
	if err := d.UpdateTaskProgress("task-1", 512, 1024, 1, 1); err != nil {
		t.Fatalf("UpdateTaskProgress failed: %v", err)
	}
	progressed, err := d.GetTask("task-1")
	if err != nil || progressed.ProgressBytes != 512 {
		t.Fatalf("expected 512 progress bytes, got: %d", progressed.ProgressBytes)
	}

	// 5. Fail task
	if err := d.UpdateTaskStatus("task-1", "failed", "simulated error"); err != nil {
		t.Fatalf("UpdateTaskStatus failed: %v", err)
	}

	// 6. Retry task
	retried, err := d.RetryTask("task-1")
	if err != nil {
		t.Fatalf("RetryTask failed: %v", err)
	}
	if retried.Status != "pending" || retried.ErrorMessage != "" {
		t.Fatalf("expected retried task to be pending, got: %+v", retried)
	}

	// 7. Reset interrupted tasks
	// Claim it again
	claimed3, _ := d.GetNextPendingTask()
	if claimed3 == nil || claimed3.Status != "running" {
		t.Fatalf("expected task running, got %+v", claimed3)
	}
	if err := d.ResetInterruptedTasks(); err != nil {
		t.Fatalf("ResetInterruptedTasks failed: %v", err)
	}
	interrupted, _ := d.GetTask("task-1")
	if interrupted.Status != "interrupted" {
		t.Fatalf("expected task interrupted, got %s", interrupted.Status)
	}

	// 8. Clear finished tasks
	if err := d.ClearFinishedTasks(); err != nil {
		t.Fatalf("ClearFinishedTasks failed: %v", err)
	}
	cleared, _ := d.GetTask("task-1")
	if cleared != nil {
		t.Fatalf("expected task-1 cleared, got %+v", cleared)
	}

	// 9. Test rolling limit trigger (cap 100 finished)
	tx, err := d.conn.Begin()
	if err != nil {
		t.Fatalf("failed to begin tx: %v", err)
	}
	for i := 1; i <= 105; i++ {
		tID := fmt.Sprintf("task-roll-%d", i)
		_, _ = tx.Exec(`
			INSERT INTO storage_tasks (id, type, status, created_at, updated_at)
			VALUES (?, 'transfer', 'pending', ?, ?)
		`, tID, time.Now().UTC(), time.Now().UTC())
		_, _ = tx.Exec(`
			UPDATE storage_tasks SET status = 'completed', updated_at = ? WHERE id = ?
		`, time.Now().UTC(), tID)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("failed to commit tx: %v", err)
	}
	tasks, err := d.ListTasks(200)
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}
	if len(tasks) > 100 {
		t.Fatalf("expected at most 100 tasks retained by trigger, got %d", len(tasks))
	}
}
