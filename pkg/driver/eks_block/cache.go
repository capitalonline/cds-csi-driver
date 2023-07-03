package eks_block

import (
	"github.com/container-storage-interface/spec/lib/go/csi"
	"sync"
)

type PvcInfo struct {
	VolumeInfo *csi.Volume
	Status     string
}

type BlockInfo struct {
	PvName string
	Status string
}

type PvcCreatedSafeMap struct {
	sync.RWMutex
	M map[string]*PvcInfo
}

func (pvc *PvcCreatedSafeMap) SaveVolume(name string, data *PvcInfo) {
	pvc.Lock()
	defer pvc.Unlock()
	pvc.M[name] = data
}

func (pvc *PvcCreatedSafeMap) GetPvc(name string) (*PvcInfo, bool) {
	pvc.RLock()
	defer pvc.RUnlock()
	data, ok := pvc.M[name]
	return data, ok
}

type BlockSafeMap struct {
	sync.RWMutex
	M map[string]*BlockInfo
}

var (
	pvcCreatedSafeMap = &PvcCreatedSafeMap{M: make(map[string]*PvcInfo)}
	blockSafeMap      = &BlockSafeMap{M: make(map[string]*BlockInfo)}
)
