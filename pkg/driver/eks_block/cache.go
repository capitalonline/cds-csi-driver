package eks_block

import "sync"

// CacheLockMap id资源（内存）锁
var CacheLockMap = new(sync.Map)

var diskExpandMap = new(sync.Map)
