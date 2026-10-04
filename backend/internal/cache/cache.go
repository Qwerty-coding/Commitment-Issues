package cache

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"CommitIssues/internal/parser"
)

const (
	// DefaultMaxEntries is the default in-memory LRU entry limit.
	DefaultMaxEntries = 10000
	// DefaultMaxMemoryBytes is the default in-memory cache capacity (50 MB).
	DefaultMaxMemoryBytes = 50 * 1024 * 1024
	// DefaultMaxDiskBytes is the default on-disk cache capacity (100 MB).
	DefaultMaxDiskBytes = 100 * 1024 * 1024
)

// Key uniquely identifies an immutable AST cache entry.
type Key struct {
	RepoID        string `json:"repoId"`
	ContentHash   string `json:"contentHash"`
	RelPath       string `json:"relPath"`
	Language      string `json:"language"`
	ParserVersion string `json:"parserVersion"`
	SchemaVersion string `json:"schemaVersion"`
}

// HashKey computes a deterministic, collision-resistant SHA-256 identifier for the key.
func (k Key) HashKey() string {
	h := sha256.New()
	cleanPath := filepath.ToSlash(filepath.Clean(k.RelPath))
	fmt.Fprintf(h, "repo:%s|hash:%s|path:%s|lang:%s|pver:%s|sver:%s",
		k.RepoID, k.ContentHash, cleanPath, k.Language, k.ParserVersion, k.SchemaVersion)
	return hex.EncodeToString(h.Sum(nil))
}

// String returns a compact, human-readable representation of the cache key.
func (k Key) String() string {
	shortHash := k.ContentHash
	if len(shortHash) > 8 {
		shortHash = shortHash[:8]
	}
	return fmt.Sprintf("%s:%s@%s[%s:%s]", k.RepoID, filepath.ToSlash(k.RelPath), shortHash, k.ParserVersion, k.SchemaVersion)
}

// Entry is an immutable cached AST unit with extraction metadata and diagnostics.
// Secrets, AI prompts, and transient run configurations are never stored here.
type Entry struct {
	Key         Key               `json:"key"`
	AST         parser.ASTContext `json:"ast"`
	ContentHash string            `json:"contentHash"`
	CachedAt    time.Time         `json:"cachedAt"`
	SizeBytes   int64             `json:"sizeBytes"`
}

// Clone returns an isolated deep copy of the entry to ensure callers cannot mutate cached state.
func (e *Entry) Clone() *Entry {
	if e == nil {
		return nil
	}
	return &Entry{
		Key:         e.Key,
		AST:         e.AST.Clone(),
		ContentHash: e.ContentHash,
		CachedAt:    e.CachedAt,
		SizeBytes:   e.SizeBytes,
	}
}

// Stats captures cache operations, hits, misses, evictions, and recovered corruptions.
type Stats struct {
	Hits        int64 `json:"hits"`
	Misses      int64 `json:"misses"`
	Stores      int64 `json:"stores"`
	Evictions   int64 `json:"evictions"`
	Corruptions int64 `json:"corruptions"`
}

// HitRatio calculates the cache hit ratio (0.0 to 1.0).
func (s Stats) HitRatio() float64 {
	total := s.Hits + s.Misses
	if total == 0 {
		return 0.0
	}
	return float64(s.Hits) / float64(total)
}

// Config configures the AST cache limits and storage modes.
type Config struct {
	MaxEntries     int    `json:"maxEntries"`
	MaxMemoryBytes int64  `json:"maxMemoryBytes"`
	EnableDisk     bool   `json:"enableDisk"`
	DiskDir        string `json:"diskDir,omitempty"`
	MaxDiskBytes   int64  `json:"maxDiskBytes"`
	ParserVersion  string `json:"parserVersion"`
	SchemaVersion  string `json:"schemaVersion"`
}

// DefaultConfig returns safe, high-performance default cache configuration.
func DefaultConfig() Config {
	return Config{
		MaxEntries:     DefaultMaxEntries,
		MaxMemoryBytes: DefaultMaxMemoryBytes,
		EnableDisk:     false,
		DiskDir:        "",
		MaxDiskBytes:   DefaultMaxDiskBytes,
		ParserVersion:  parser.CurrentParserVersion,
		SchemaVersion:  parser.CurrentSchemaVersion,
	}
}

// Cache defines the public interface for AST caching and incremental parsing.
type Cache interface {
	Get(key Key) (*Entry, bool)
	Put(entry *Entry) error
	GetOrParse(ctx context.Context, repoID, relPath string, source []byte) (parser.ASTContext, bool, error)
	Invalidate(predicate func(key Key) bool) int
	InvalidateFile(repoID, relPath string) int
	// InvalidateDependents conservatively invalidates cached entries whose
	// imports reference changedRelPath (dependents only, not the file itself).
	InvalidateDependents(repoID, changedRelPath string) int
	// InvalidateFileDependencyAware invalidates a changed file and every cached
	// entry that imports it.
	InvalidateFileDependencyAware(repoID, relPath string) int
	Clear() error
	Stats() Stats
	ResetStats()
	Close() error
}

type lruItem struct {
	key   string
	entry *Entry
}

// ASTCache implements bounded process-local LRU caching and optional persistent disk storage.
type ASTCache struct {
	mu          sync.RWMutex
	config      Config
	items       map[string]*list.Element
	lru         *list.List
	curMemory   int64
	hits        atomic.Int64
	misses      atomic.Int64
	stores      atomic.Int64
	evictions   atomic.Int64
	corruptions atomic.Int64
	fileLocks   sync.Map // key string -> *sync.Mutex
}

// New creates a new ASTCache configured with the provided options.
func New(cfg Config) (*ASTCache, error) {
	if cfg.MaxEntries <= 0 {
		cfg.MaxEntries = DefaultMaxEntries
	}
	if cfg.MaxMemoryBytes <= 0 {
		cfg.MaxMemoryBytes = DefaultMaxMemoryBytes
	}
	if cfg.MaxDiskBytes <= 0 {
		cfg.MaxDiskBytes = DefaultMaxDiskBytes
	}
	if cfg.ParserVersion == "" {
		cfg.ParserVersion = parser.CurrentParserVersion
	}
	if cfg.SchemaVersion == "" {
		cfg.SchemaVersion = parser.CurrentSchemaVersion
	}
	if cfg.EnableDisk && cfg.DiskDir != "" {
		if err := os.MkdirAll(cfg.DiskDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to initialize disk cache directory %q: %w", cfg.DiskDir, err)
		}
	}

	return &ASTCache{
		config: cfg,
		items:  make(map[string]*list.Element),
		lru:    list.New(),
	}, nil
}

// NewDefault returns an in-memory process-local ASTCache with standard bounds.
func NewDefault() *ASTCache {
	c, _ := New(DefaultConfig())
	return c
}

func (c *ASTCache) getFileLock(keyHash string) *sync.Mutex {
	val, _ := c.fileLocks.LoadOrStore(keyHash, &sync.Mutex{})
	return val.(*sync.Mutex)
}

// Get retrieves a cached ASTContext by Key, checking memory first then persistent disk.
func (c *ASTCache) Get(key Key) (*Entry, bool) {
	hashKey := key.HashKey()

	// 1. Process-local memory cache lookup
	c.mu.Lock()
	if elem, found := c.items[hashKey]; found {
		item := elem.Value.(*lruItem)
		// Verify parser and schema version matches
		if item.entry.Key.ParserVersion == key.ParserVersion &&
			item.entry.Key.SchemaVersion == key.SchemaVersion {
			c.lru.MoveToFront(elem)
			c.hits.Add(1)
			cloned := item.entry.Clone()
			c.mu.Unlock()
			return cloned, true
		}
		// Expired / mismatched schema version in memory
		c.removeElement(elem)
	}
	c.mu.Unlock()

	// 2. Persistent disk cache lookup (if enabled)
	if c.config.EnableDisk && c.config.DiskDir != "" {
		entry, hit := c.readDiskEntry(key, hashKey)
		if hit {
			c.hits.Add(1)
			// Promote to memory cache
			c.mu.Lock()
			c.putMemoryLocked(entry)
			c.mu.Unlock()
			return entry.Clone(), true
		}
	}

	c.misses.Add(1)
	return nil, false
}

// Put atomically publishes an immutable Entry to memory and disk cache.
func (c *ASTCache) Put(entry *Entry) error {
	if entry == nil {
		return errors.New("cannot cache nil entry")
	}
	if entry.Key.ParserVersion == "" {
		entry.Key.ParserVersion = c.config.ParserVersion
	}
	if entry.Key.SchemaVersion == "" {
		entry.Key.SchemaVersion = c.config.SchemaVersion
	}
	if entry.SizeBytes <= 0 {
		entry.SizeBytes = estimateEntrySize(entry)
	}
	if entry.CachedAt.IsZero() {
		entry.CachedAt = time.Now().UTC()
	}

	// 1. Store into memory cache
	c.mu.Lock()
	c.putMemoryLocked(entry)
	c.stores.Add(1)
	c.mu.Unlock()

	// 2. Store into persistent disk cache (if enabled)
	if c.config.EnableDisk && c.config.DiskDir != "" {
		if err := c.writeDiskEntry(entry); err != nil {
			return err
		}
	}
	return nil
}

func (c *ASTCache) putMemoryLocked(entry *Entry) {
	hashKey := entry.Key.HashKey()
	if elem, found := c.items[hashKey]; found {
		oldItem := elem.Value.(*lruItem)
		c.curMemory -= oldItem.entry.SizeBytes
		oldItem.entry = entry.Clone()
		c.curMemory += entry.SizeBytes
		c.lru.MoveToFront(elem)
		return
	}

	cloned := entry.Clone()
	elem := c.lru.PushFront(&lruItem{
		key:   hashKey,
		entry: cloned,
	})
	c.items[hashKey] = elem
	c.curMemory += entry.SizeBytes

	// Evict entries exceeding bounds
	for (len(c.items) > c.config.MaxEntries || (c.curMemory > c.config.MaxMemoryBytes && len(c.items) > 1)) && c.lru.Len() > 0 {
		back := c.lru.Back()
		if back == nil {
			break
		}
		c.removeElement(back)
		c.evictions.Add(1)
	}
}

func (c *ASTCache) removeElement(elem *list.Element) {
	item := elem.Value.(*lruItem)
	delete(c.items, item.key)
	c.lru.Remove(elem)
	c.curMemory -= item.entry.SizeBytes
	if c.curMemory < 0 {
		c.curMemory = 0
	}
}

// GetOrParse incrementally analyzes source code. If cached AST exists for the
// file and content hash, it is reused immediately. Otherwise, ParseAndExtract is run,
// and published atomically upon success without leaving partial entries on cancellation.
func (c *ASTCache) GetOrParse(ctx context.Context, repoID, relPath string, source []byte) (parser.ASTContext, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return parser.ASTContext{}, false, err
	}

	contentHash := parser.HashSource(source)
	lang := parser.GetLanguageName(relPath)
	key := Key{
		RepoID:        repoID,
		ContentHash:   contentHash,
		RelPath:       filepath.Clean(relPath),
		Language:      lang,
		ParserVersion: c.config.ParserVersion,
		SchemaVersion: c.config.SchemaVersion,
	}

	// 1. Cache hit
	if entry, found := c.Get(key); found {
		return entry.AST, true, nil
	}

	// Cancellation check prior to parser invocation
	if err := ctx.Err(); err != nil {
		return parser.ASTContext{}, false, err
	}

	// 2. Incremental reparse
	astCtx, parseErr := parser.ParseAndExtract(relPath, source)
	if parseErr != nil && !errors.Is(parseErr, parser.ErrFileTooLarge) && !errors.Is(parseErr, parser.ErrBinaryFile) {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return parser.ASTContext{}, false, ctxErr
		}
	}

	// Guarantee: cancellation does NOT leave partial entries in cache
	if err := ctx.Err(); err != nil {
		return parser.ASTContext{}, false, err
	}

	// 3. Atomic publication upon completion
	if parseErr == nil || errors.Is(parseErr, parser.ErrFileTooLarge) || errors.Is(parseErr, parser.ErrBinaryFile) {
		entry := &Entry{
			Key:         key,
			AST:         astCtx,
			ContentHash: contentHash,
			CachedAt:    time.Now().UTC(),
		}
		_ = c.Put(entry)
	}

	return astCtx, false, parseErr
}

// Invalidate removes entries matching the predicate from both memory and disk.
func (c *ASTCache) Invalidate(predicate func(key Key) bool) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	count := 0
	for k, elem := range c.items {
		item := elem.Value.(*lruItem)
		if predicate(item.entry.Key) {
			delete(c.items, k)
			c.lru.Remove(elem)
			c.curMemory -= item.entry.SizeBytes
			count++

			// Remove from disk if enabled
			if c.config.EnableDisk && c.config.DiskDir != "" {
				filePath := filepath.Join(c.config.DiskDir, item.key+".json")
				_ = os.Remove(filePath)
			}
		}
	}
	if c.curMemory < 0 {
		c.curMemory = 0
	}
	return count
}

// InvalidateFile invalidates all cached entries for a specific file path within a repository.
func (c *ASTCache) InvalidateFile(repoID, relPath string) int {
	clean := filepath.Clean(relPath)
	return c.Invalidate(func(k Key) bool {
		return k.RepoID == repoID && filepath.Clean(k.RelPath) == clean
	})
}

// Clear clears all in-memory and on-disk cached entries.
func (c *ASTCache) Clear() error {
	c.mu.Lock()
	c.items = make(map[string]*list.Element)
	c.lru.Init()
	c.curMemory = 0
	c.mu.Unlock()

	if c.config.EnableDisk && c.config.DiskDir != "" {
		entries, err := os.ReadDir(c.config.DiskDir)
		if err == nil {
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), ".json") || strings.HasSuffix(e.Name(), ".tmp") {
					_ = os.Remove(filepath.Join(c.config.DiskDir, e.Name()))
				}
			}
		}
	}
	return nil
}

// Stats returns current operational metrics.
func (c *ASTCache) Stats() Stats {
	return Stats{
		Hits:        c.hits.Load(),
		Misses:      c.misses.Load(),
		Stores:      c.stores.Load(),
		Evictions:   c.evictions.Load(),
		Corruptions: c.corruptions.Load(),
	}
}

// ResetStats zeroes the operational metrics counters.
func (c *ASTCache) ResetStats() {
	c.hits.Store(0)
	c.misses.Store(0)
	c.stores.Store(0)
	c.evictions.Store(0)
	c.corruptions.Store(0)
}

// Close gracefully shuts down the cache.
func (c *ASTCache) Close() error {
	return nil
}

// --- Persistent Disk Storage & Corruption Recovery ---

type diskPayload struct {
	Entry    *Entry `json:"entry"`
	Checksum string `json:"checksum"`
}

func (c *ASTCache) readDiskEntry(key Key, hashKey string) (*Entry, bool) {
	filePath := filepath.Join(c.config.DiskDir, hashKey+".json")

	fileLock := c.getFileLock(hashKey)
	fileLock.Lock()
	defer fileLock.Unlock()

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, false
	}

	var payload diskPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		// Corrupt JSON file detected: recover safely by deleting corrupt file
		c.corruptions.Add(1)
		_ = os.Remove(filePath)
		return nil, false
	}

	if payload.Entry == nil {
		c.corruptions.Add(1)
		_ = os.Remove(filePath)
		return nil, false
	}

	// Verify checksum to guard against silent disk truncation/corruption
	expectedChecksum := computePayloadChecksum(payload.Entry)
	if payload.Checksum != expectedChecksum {
		c.corruptions.Add(1)
		_ = os.Remove(filePath)
		return nil, false
	}

	// Verify schema and parser versioning
	if payload.Entry.Key.SchemaVersion != key.SchemaVersion ||
		payload.Entry.Key.ParserVersion != key.ParserVersion {
		_ = os.Remove(filePath)
		return nil, false
	}

	return payload.Entry, true
}

func (c *ASTCache) writeDiskEntry(entry *Entry) error {
	hashKey := entry.Key.HashKey()
	targetFile := filepath.Join(c.config.DiskDir, hashKey+".json")

	fileLock := c.getFileLock(hashKey)
	fileLock.Lock()
	defer fileLock.Unlock()

	payload := diskPayload{
		Entry:    entry,
		Checksum: computePayloadChecksum(entry),
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal disk entry: %w", err)
	}

	// Atomic write via temporary file
	tempFile := filepath.Join(c.config.DiskDir, fmt.Sprintf("%s.%d.tmp", hashKey, time.Now().UnixNano()))
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp disk cache file: %w", err)
	}

	// Safe rename on Windows and Unix
	_ = os.Remove(targetFile)
	if err := os.Rename(tempFile, targetFile); err != nil {
		_ = os.Remove(tempFile)
		return fmt.Errorf("failed to atomically commit disk cache file: %w", err)
	}

	// Enforce disk size limit and evict oldest if needed
	c.enforceDiskLimits()
	return nil
}

func (c *ASTCache) enforceDiskLimits() {
	if c.config.MaxDiskBytes <= 0 || c.config.DiskDir == "" {
		return
	}

	entries, err := os.ReadDir(c.config.DiskDir)
	if err != nil {
		return
	}

	type fileInfo struct {
		path    string
		size    int64
		modTime time.Time
	}

	var files []fileInfo
	var totalSize int64

	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fileInfo{
			path:    filepath.Join(c.config.DiskDir, e.Name()),
			size:    info.Size(),
			modTime: info.ModTime(),
		})
		totalSize += info.Size()
	}

	if totalSize <= c.config.MaxDiskBytes {
		return
	}

	// Sort oldest first for eviction
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.Before(files[j].modTime)
	})

	targetSize := int64(float64(c.config.MaxDiskBytes) * 0.8)
	for _, f := range files {
		if totalSize <= targetSize {
			break
		}
		if err := os.Remove(f.path); err == nil {
			totalSize -= f.size
			c.evictions.Add(1)
		}
	}
}

func computePayloadChecksum(entry *Entry) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%s|%s|%s|%s|%d|%d",
		entry.Key.RepoID,
		entry.Key.ContentHash,
		entry.Key.RelPath,
		entry.Key.Language,
		entry.Key.ParserVersion,
		entry.Key.SchemaVersion,
		len(entry.AST.Functions),
		len(entry.AST.Variables),
	)
	return hex.EncodeToString(h.Sum(nil))
}

func estimateEntrySize(entry *Entry) int64 {
	size := int64(128) // base struct overhead
	size += int64(len(entry.Key.RepoID) + len(entry.Key.ContentHash) + len(entry.Key.RelPath))
	size += int64(len(entry.AST.Language) + len(entry.AST.ParserName) + len(entry.AST.ParserVer))

	for _, f := range entry.AST.Functions {
		size += int64(len(f.Name) + len(f.Kind) + len(f.Content) + len(f.Scope) + len(f.Signature) + 64)
	}
	for _, v := range entry.AST.Variables {
		size += int64(len(v.Name) + len(v.Kind) + len(v.Content) + len(v.Scope) + 64)
	}
	for _, imp := range entry.AST.Imports {
		size += int64(len(imp.Name) + len(imp.Content) + 64)
	}
	for _, d := range entry.AST.Diagnostics {
		size += int64(len(d.Message) + len(d.Severity) + 32)
	}
	return size
}
