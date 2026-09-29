package storage

import (
	"context"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/herliansyah/cloudgate/pkg/db"
)

// TaskManager coordinates asynchronous background execution of storage tasks.
type TaskManager struct {
	database     *db.DB
	trashManager *TrashManager
	getDriver    func(accountID string) (Driver, error)
	workers      int
	wakeCh       chan struct{}
	mu           sync.Mutex
	cancels      map[string]context.CancelFunc
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

// NewTaskManager creates a background task manager configured for 2 concurrent workers.
func NewTaskManager(
	database *db.DB,
	trashManager *TrashManager,
	getDriver func(accountID string) (Driver, error),
) *TaskManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &TaskManager{
		database:     database,
		trashManager: trashManager,
		getDriver:    getDriver,
		workers:      2, // ponytail: 2 concurrent workers avoids rate-limiting and high RAM
		wakeCh:       make(chan struct{}, 10),
		cancels:      make(map[string]context.CancelFunc),
		ctx:          ctx,
		cancel:       cancel,
	}
}

// Start resets interrupted tasks from prior runs and launches background worker goroutines.
func (tm *TaskManager) Start() error {
	if err := tm.database.ResetInterruptedTasks(); err != nil {
		return fmt.Errorf("failed to reset interrupted tasks: %w", err)
	}

	for i := 0; i < tm.workers; i++ {
		tm.wg.Add(1)
		go tm.workerLoop(i)
	}
	return nil
}

// Stop terminates all workers and cancels currently executing tasks.
func (tm *TaskManager) Stop() {
	tm.cancel()

	tm.mu.Lock()
	for _, c := range tm.cancels {
		c()
	}
	tm.cancels = make(map[string]context.CancelFunc)
	tm.mu.Unlock()

	tm.wg.Wait()
}

// Notify wakes up workers when new tasks arrive.
func (tm *TaskManager) Notify() {
	select {
	case tm.wakeCh <- struct{}{}:
	default:
	}
}

// Enqueue persists a new task in SQLite and wakes up workers.
func (tm *TaskManager) Enqueue(task *db.StorageTask) error {
	if err := tm.database.CreateTask(task); err != nil {
		return err
	}
	tm.Notify()
	return nil
}

// Cancel terminates a running or pending task.
func (tm *TaskManager) Cancel(taskID string) error {
	tm.mu.Lock()
	cancelFn, running := tm.cancels[taskID]
	if running {
		cancelFn()
		delete(tm.cancels, taskID)
	}
	tm.mu.Unlock()

	return tm.database.UpdateTaskStatus(taskID, "cancelled", "Cancelled by user")
}

// Retry re-enqueues a failed, cancelled, or interrupted task.
func (tm *TaskManager) Retry(taskID string) (*db.StorageTask, error) {
	task, err := tm.database.RetryTask(taskID)
	if err != nil {
		return nil, err
	}
	tm.Notify()
	return task, nil
}

func (tm *TaskManager) workerLoop(workerID int) {
	defer tm.wg.Done()

	for {
		select {
		case <-tm.ctx.Done():
			return
		default:
		}

		task, err := tm.database.GetNextPendingTask()
		if err != nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		if task == nil {
			select {
			case <-tm.ctx.Done():
				return
			case <-tm.wakeCh:
				continue
			case <-time.After(2 * time.Second):
				continue
			}
		}

		tm.executeTask(task)
	}
}

func (tm *TaskManager) executeTask(task *db.StorageTask) {
	taskCtx, taskCancel := context.WithCancel(tm.ctx)

	tm.mu.Lock()
	tm.cancels[task.ID] = taskCancel
	tm.mu.Unlock()

	defer func() {
		taskCancel()
		tm.mu.Lock()
		delete(tm.cancels, task.ID)
		tm.mu.Unlock()
	}()

	var execErr error
	var summaryErrors []string

	switch task.Type {
	case "transfer":
		execErr, summaryErrors = tm.handleTransfer(taskCtx, task)
	case "replicate":
		execErr, summaryErrors = tm.handleReplication(taskCtx, task)
	case "ingest":
		execErr = tm.handleIngest(taskCtx, task)
	default:
		execErr = fmt.Errorf("unknown task type: %s", task.Type)
	}

	if taskCtx.Err() != nil {
		_ = tm.database.UpdateTaskStatus(task.ID, "cancelled", "Operation was cancelled")
		return
	}

	if execErr != nil {
		_ = tm.database.UpdateTaskStatus(task.ID, "failed", execErr.Error())
		return
	}

	if len(summaryErrors) > 0 {
		errMsg := fmt.Sprintf("Completed with %d error(s): %s", len(summaryErrors), strings.Join(summaryErrors, "; "))
		_ = tm.database.UpdateTaskStatus(task.ID, "completed", errMsg)
	} else {
		_ = tm.database.UpdateTaskStatus(task.ID, "completed", "")
	}
}

func (tm *TaskManager) handleTransfer(ctx context.Context, task *db.StorageTask) (error, []string) {
	srcDriver, err := tm.getDriver(task.SourceAccountID)
	if err != nil {
		return fmt.Errorf("source driver error: %w", err), nil
	}
	dstDriver, err := tm.getDriver(task.TargetAccountID)
	if err != nil {
		return fmt.Errorf("target driver error: %w", err), nil
	}

	onProgress := func(pBytes, tBytes int64, itemsP, itemsT int) {
		_ = tm.database.UpdateTaskProgress(task.ID, pBytes, tBytes, itemsP, itemsT)
	}

	if task.IsDir {
		_, _, errLog, err := TransferFolder(ctx, srcDriver, task.SourcePath, dstDriver, task.TargetPath, task.IsMove, onProgress)
		return err, errLog
	}

	// Single file transfer
	srcInfo, err := Stat(ctx, srcDriver, task.SourcePath)
	if err != nil {
		return fmt.Errorf("cannot access source file: %w", err), nil
	}

	onProgress(0, srcInfo.Size, 0, 1)

	// Ensure destination directory exists
	targetDir := path.Dir(task.TargetPath)
	if targetDir != "" && targetDir != "." && targetDir != "/" {
		_ = dstDriver.Mkdir(ctx, targetDir)
	}

	if task.IsMove {
		if err := MoveFile(ctx, srcDriver, task.SourcePath, dstDriver, task.TargetPath); err != nil {
			return err, nil
		}
	} else {
		if err := CopyFile(ctx, srcDriver, task.SourcePath, dstDriver, task.TargetPath); err != nil {
			return err, nil
		}
	}

	onProgress(srcInfo.Size, srcInfo.Size, 1, 1)
	return nil, nil
}

func (tm *TaskManager) handleReplication(ctx context.Context, task *db.StorageTask) (error, []string) {
	srcDriver, err := tm.getDriver(task.SourceAccountID)
	if err != nil {
		return fmt.Errorf("source driver error: %w", err), nil
	}
	dstDriver, err := tm.getDriver(task.TargetAccountID)
	if err != nil {
		return fmt.Errorf("target driver error: %w", err), nil
	}

	onProgress := func(pBytes, tBytes int64, itemsP, itemsT int) {
		_ = tm.database.UpdateTaskProgress(task.ID, pBytes, tBytes, itemsP, itemsT)
	}

	_, _, errLog, err := ReplicateFolder(ctx, srcDriver, task.SourcePath, dstDriver, task.TargetPath, task.Mirror, tm.trashManager, onProgress)
	return err, errLog
}

func (tm *TaskManager) handleIngest(ctx context.Context, task *db.StorageTask) error {
	dstDriver, err := tm.getDriver(task.TargetAccountID)
	if err != nil {
		return fmt.Errorf("target driver error: %w", err)
	}

	// For ingest: SourcePath is the URL, TargetPath is destination folder (or file path)
	targetDir := task.TargetPath
	customName := ""
	if !task.IsDir && path.Ext(task.TargetPath) != "" {
		targetDir = path.Dir(task.TargetPath)
		customName = path.Base(task.TargetPath)
	}

	onProgress := func(readBytes, totalBytes int64) {
		_ = tm.database.UpdateTaskProgress(task.ID, readBytes, totalBytes, 0, 1)
	}

	finalPath, readBytes, err := RemoteIngestStream(ctx, task.SourcePath, customName, dstDriver, targetDir, onProgress)
	if err != nil {
		return err
	}

	_ = tm.database.UpdateTaskProgress(task.ID, readBytes, readBytes, 1, 1)
	task.TargetPath = finalPath
	return nil
}
