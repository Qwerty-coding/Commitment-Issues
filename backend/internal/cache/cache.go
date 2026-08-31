package cache

import (
	"sync"
)

// simple in-memory cache for report JSON bytes keyed by repo+file
var (
	mu    sync.Mutex
	store map[string][]byte
)

func init() {
	store = make(map[string][]byte)
}

func makeKey(repoRoot, fileName string) string {
	return repoRoot + "|" + fileName
}

// SaveReport stores jsonBytes in memory and returns the key.
func SaveReport(repoRoot, fileName string, jsonBytes []byte) string {
	key := makeKey(repoRoot, fileName)
	mu.Lock()
	store[key] = append([]byte(nil), jsonBytes...)
	mu.Unlock()
	return key
}

// GetReport retrieves the report bytes by repoRoot+fileName. Returns nil,false if missing.
func GetReport(repoRoot, fileName string) ([]byte, bool) {
	key := makeKey(repoRoot, fileName)
	mu.Lock()
	b, ok := store[key]
	mu.Unlock()
	if !ok {
		return nil, false
	}
	return append([]byte(nil), b...), true
}

// DeleteReport removes the cached entry.
func DeleteReport(repoRoot, fileName string) {
	key := makeKey(repoRoot, fileName)
	mu.Lock()
	delete(store, key)
	mu.Unlock()
}
