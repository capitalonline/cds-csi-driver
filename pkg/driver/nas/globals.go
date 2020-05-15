package nas


import "sync"

var (
	RR     = make(map[string]uint, 0)
	RRLock = sync.Mutex{}
)