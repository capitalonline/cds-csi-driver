package utils

import (
	"github.com/capitalonline/cds-csi-driver/pkg/sets"
	"sync"
)

const (
	// VolumeOperationAlreadyExistsFmt string format to return for concurrent operation.
	VolumeOperationAlreadyExistsFmt = "an operation with the given Volume ID %s already exists"

	// SnapshotOperationAlreadyExistsFmt string format to return for concurrent operation.
	SnapshotOperationAlreadyExistsFmt = "an operation with the given Snapshot ID %s already exists"
)

// VolumeLocks implements a map with atomic operations. It stores a set of all volume IDs
// with an ongoing operation.
type VolumeLocks struct {
	locks sets.Set[string]
	mux   sync.Mutex
}

// NewVolumeLocks returns new VolumeLocks.
func NewVolumeLocks() *VolumeLocks {
	return &VolumeLocks{
		locks: sets.New[string](),
	}
}

// TryAcquire tries to acquire the lock for operating on volumeID and returns true if successful.
// If another operation is already using volumeID, returns false.
func (vl *VolumeLocks) TryAcquire(volumeID string) bool {
	vl.mux.Lock()
	defer vl.mux.Unlock()
	if vl.locks.Has(volumeID) {
		return false
	}
	vl.locks.Insert(volumeID)

	return true
}

// Release deletes the lock on volumeID.
func (vl *VolumeLocks) Release(volumeID string) {
	vl.mux.Lock()
	defer vl.mux.Unlock()
	vl.locks.Delete(volumeID)
}
