package storage

import (
	"context"
	"fmt"
	"io"
	"sync"
)

// StoragePool aggregates multiple Drivers and distributes writes using capacity-aware round-robin.
type StoragePool struct {
	mu        sync.Mutex
	id        string
	name      string
	drivers   []Driver
	nextIndex int
}

func NewStoragePool(id, name string, drivers []Driver) *StoragePool {
	return &StoragePool{
		id:      id,
		name:    name,
		drivers: drivers,
	}
}

func (p *StoragePool) ID() string   { return p.id }
func (p *StoragePool) Name() string { return p.name }

// AllocateTargetDriver selects the next suitable driver in the pool using capacity-aware round-robin.
func (p *StoragePool) AllocateTargetDriver(ctx context.Context, fileSize int64) (Driver, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.drivers) == 0 {
		return nil, ErrInsufficientCapacity
	}

	totalDrivers := len(p.drivers)
	startIndex := p.nextIndex

	for i := 0; i < totalDrivers; i++ {
		candidateIndex := (startIndex + i) % totalDrivers
		candidate := p.drivers[candidateIndex]

		quota, err := candidate.About(ctx)
		if err == nil && quota.Free >= fileSize {
			// Found a healthy driver with enough quota
			p.nextIndex = (candidateIndex + 1) % totalDrivers
			return candidate, nil
		}
	}

	return nil, fmt.Errorf("%w: no drive in pool has %d bytes free", ErrInsufficientCapacity, fileSize)
}

// Write streams a new file into the pool, automatically selecting the target driver.
func (p *StoragePool) Write(ctx context.Context, filePath string, in io.Reader, fileSize int64) (string, error) {
	targetDriver, err := p.AllocateTargetDriver(ctx, fileSize)
	if err != nil {
		return "", err
	}

	if err := targetDriver.Put(ctx, filePath, in, fileSize); err != nil {
		return "", fmt.Errorf("failed to write to target driver %s: %w", targetDriver.ID(), err)
	}

	return targetDriver.ID(), nil
}

// UnifiedList aggregates files from all drivers in the pool into a unified virtual listing.
func (p *StoragePool) UnifiedList(ctx context.Context, dirPath string) ([]FileInfo, error) {
	p.mu.Lock()
	drivers := make([]Driver, len(p.drivers))
	copy(drivers, p.drivers)
	p.mu.Unlock()

	var allFiles []FileInfo
	seenPaths := make(map[string]bool)

	for _, d := range drivers {
		files, err := d.List(ctx, dirPath)
		if err != nil {
			// Skip disconnected drivers gracefully
			continue
		}
		for _, f := range files {
			if !seenPaths[f.Path] {
				seenPaths[f.Path] = true
				allFiles = append(allFiles, f)
			}
		}
	}

	return allFiles, nil
}

// AddDriver adds or updates a driver in the pool.
func (p *StoragePool) AddDriver(d Driver) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, existing := range p.drivers {
		if existing.ID() == d.ID() {
			p.drivers[i] = d
			return
		}
	}
	p.drivers = append(p.drivers, d)
}

// RemoveDriver removes a driver from the pool by its account ID.
func (p *StoragePool) RemoveDriver(accountID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var updated []Driver
	for _, d := range p.drivers {
		if d.ID() != accountID {
			updated = append(updated, d)
		}
	}
	p.drivers = updated
}
