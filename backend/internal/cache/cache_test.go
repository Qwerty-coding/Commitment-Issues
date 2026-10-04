package cache_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"CommitIssues/internal/cache"
	"CommitIssues/internal/parser"
)

// 1. Same content produces a cache hit.
func TestCache_SameContent_ProducesCacheHit(t *testing.T) {
	c := cache.NewDefault()
	source := []byte("package main\n\nfunc Hello() string { return \"world\" }\n")
	repoID := "repo-test-1"
	relPath := "main.go"

	// Cold call: should parse and miss cache
	ast1, hit1, err := c.GetOrParse(context.Background(), repoID, relPath, source)
	if err != nil {
		t.Fatalf("first GetOrParse failed: %v", err)
	}
	if hit1 {
		t.Fatalf("expected first call to be a cache miss, got hit")
	}
	if len(ast1.Functions) == 0 {
		t.Fatalf("expected extracted functions in ast1")
	}

	stats := c.Stats()
	if stats.Misses != 1 || stats.Hits != 0 || stats.Stores != 1 {
		t.Fatalf("unexpected stats after cold call: %+v", stats)
	}

	// Warm call: same content should produce a cache hit
	ast2, hit2, err := c.GetOrParse(context.Background(), repoID, relPath, source)
	if err != nil {
		t.Fatalf("second GetOrParse failed: %v", err)
	}
	if !hit2 {
		t.Fatalf("expected second call to be a cache hit, got miss")
	}

	stats = c.Stats()
	if stats.Hits != 1 || stats.Misses != 1 {
		t.Fatalf("unexpected stats after warm call: %+v", stats)
	}
	if stats.HitRatio() != 0.5 {
		t.Fatalf("expected hit ratio 0.5, got %f", stats.HitRatio())
	}

	// Verify extracted AST data is identical
	if !reflect.DeepEqual(ast1.Functions, ast2.Functions) {
		t.Fatalf("AST functions differ between cold and warm calls")
	}
}

// 2. One changed file reparses only that file.
func TestCache_OneChangedFile_ReparsesOnlyThatFile(t *testing.T) {
	c := cache.NewDefault()
	repoID := "repo-test-2"

	fileA := "service_a.go"
	sourceA := []byte("package main\nfunc ServiceA() int { return 1 }\n")

	fileB := "service_b.go"
	sourceB := []byte("package main\nfunc ServiceB() int { return 2 }\n")

	// Initial parse of both files
	_, hitA1, err := c.GetOrParse(context.Background(), repoID, fileA, sourceA)
	if err != nil || hitA1 {
		t.Fatalf("unexpected result for file A cold call: hit=%v, err=%v", hitA1, err)
	}
	_, hitB1, err := c.GetOrParse(context.Background(), repoID, fileB, sourceB)
	if err != nil || hitB1 {
		t.Fatalf("unexpected result for file B cold call: hit=%v, err=%v", hitB1, err)
	}

	storesBefore := c.Stats().Stores
	missesBefore := c.Stats().Misses

	// Now modify file B; leave file A untouched
	sourceBModified := []byte("package main\nfunc ServiceBModified() int { return 99 }\n")

	// Second analysis pass
	astA2, hitA2, err := c.GetOrParse(context.Background(), repoID, fileA, sourceA)
	if err != nil || !hitA2 {
		t.Fatalf("file A should have been a cache hit: hit=%v, err=%v", hitA2, err)
	}
	if len(astA2.Functions) != 1 || astA2.Functions[0].Name != "ServiceA" {
		t.Fatalf("unexpected AST for file A: %+v", astA2.Functions)
	}

	astB2, hitB2, err := c.GetOrParse(context.Background(), repoID, fileB, sourceBModified)
	if err != nil || hitB2 {
		t.Fatalf("file B should have been reparsed (cache miss): hit=%v, err=%v", hitB2, err)
	}
	if len(astB2.Functions) != 1 || astB2.Functions[0].Name != "ServiceBModified" {
		t.Fatalf("unexpected AST for file B modified: %+v", astB2.Functions)
	}

	// Verify only 1 reparse occurred
	newMisses := c.Stats().Misses - missesBefore
	newStores := c.Stats().Stores - storesBefore
	if newMisses != 1 || newStores != 1 {
		t.Fatalf("expected exactly 1 reparse and 1 store, got misses=%d stores=%d", newMisses, newStores)
	}
}

// 3. Parser version invalidates old entries.
func TestCache_ParserVersion_InvalidatesOldEntries(t *testing.T) {
	cfg := cache.DefaultConfig()
	cfg.ParserVersion = "1.0.0"
	c, err := cache.New(cfg)
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}

	source := []byte("def calculate():\n    return 42\n")
	repoID := "repo-py"
	relPath := "calc.py"

	// Parse with version 1.0.0
	_, hit1, err := c.GetOrParse(context.Background(), repoID, relPath, source)
	if err != nil || hit1 {
		t.Fatalf("expected cold miss on v1.0.0")
	}

	// Verify cache hit on v1.0.0
	_, hit2, err := c.GetOrParse(context.Background(), repoID, relPath, source)
	if err != nil || !hit2 {
		t.Fatalf("expected warm hit on v1.0.0")
	}

	// Now query directly with key having ParserVersion "2.0.0"
	contentHash := parser.HashSource(source)
	keyV2 := cache.Key{
		RepoID:        repoID,
		ContentHash:   contentHash,
		RelPath:       relPath,
		Language:      "python",
		ParserVersion: "2.0.0",
		SchemaVersion: parser.CurrentSchemaVersion,
	}

	_, found := c.Get(keyV2)
	if found {
		t.Fatalf("expected entry with old parser version to be invalidated/missed for v2.0.0")
	}

	// Same for SchemaVersion
	keySchemaV2 := cache.Key{
		RepoID:        repoID,
		ContentHash:   contentHash,
		RelPath:       relPath,
		Language:      "python",
		ParserVersion: "1.0.0",
		SchemaVersion: "2.0.0",
	}
	_, foundSchema := c.Get(keySchemaV2)
	if foundSchema {
		t.Fatalf("expected entry with old schema version to be invalidated/missed")
	}
}

// 4. Corrupt cache entries recover safely.
func TestCache_CorruptCacheEntries_RecoverSafely(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "cache_corrupt_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cfg := cache.Config{
		MaxEntries:    100,
		EnableDisk:    true,
		DiskDir:       tempDir,
		MaxDiskBytes:  10 * 1024 * 1024,
		ParserVersion: "1.0.0",
		SchemaVersion: "1.0.0",
	}
	c, err := cache.New(cfg)
	if err != nil {
		t.Fatalf("failed to create disk cache: %v", err)
	}

	source := []byte("package main\nfunc RecoverTest() bool { return true }\n")
	repoID := "repo-corrupt"
	relPath := "recover.go"

	// 1. Initial parse writes to memory and disk
	_, _, err = c.GetOrParse(context.Background(), repoID, relPath, source)
	if err != nil {
		t.Fatalf("initial GetOrParse failed: %v", err)
	}

	// 2. Clear memory cache to force reading from disk
	c.Clear()
	// Re-enable disk dir since Clear removes all files in diskDir
	// Let's re-parse once so disk file is written
	_, _, err = c.GetOrParse(context.Background(), repoID, relPath, source)
	if err != nil {
		t.Fatalf("re-parse failed: %v", err)
	}

	// Evict from memory directly
	c.Invalidate(func(k cache.Key) bool { return false }) // leaves disk intact
	// Recreate cache pointing to same diskDir to ensure clean memory
	c2, err := cache.New(cfg)
	if err != nil {
		t.Fatalf("failed to create second cache: %v", err)
	}

	// Confirm it reads from disk on c2
	contentHash := parser.HashSource(source)
	key := cache.Key{
		RepoID:        repoID,
		ContentHash:   contentHash,
		RelPath:       relPath,
		Language:      "go",
		ParserVersion: "1.0.0",
		SchemaVersion: "1.0.0",
	}
	entry, found := c2.Get(key)
	if !found || entry == nil {
		t.Fatalf("expected disk cache hit before corruption")
	}

	// 3. Deliberately corrupt the file on disk
	diskFile := filepath.Join(tempDir, key.HashKey()+".json")
	if err := os.WriteFile(diskFile, []byte("{corrupt_garbage_json_data!#@$!"), 0644); err != nil {
		t.Fatalf("failed to corrupt disk file: %v", err)
	}

	// Recreate cache with empty memory
	c3, err := cache.New(cfg)
	if err != nil {
		t.Fatalf("failed to create c3: %v", err)
	}

	// 4. Reading corrupted file must not panic or error out; it must safely recover and treat as miss
	entryAfterCorrupt, foundAfterCorrupt := c3.Get(key)
	if foundAfterCorrupt || entryAfterCorrupt != nil {
		t.Fatalf("corrupted entry should not be returned")
	}
	if c3.Stats().Corruptions < 1 {
		t.Fatalf("expected corruption counter to increment, got %d", c3.Stats().Corruptions)
	}

	// 5. Subsequent GetOrParse should reparse cleanly and heal cache
	astNew, hitNew, err := c3.GetOrParse(context.Background(), repoID, relPath, source)
	if err != nil {
		t.Fatalf("GetOrParse after corruption recovery failed: %v", err)
	}
	if hitNew {
		t.Fatalf("expected reparse after corruption recovery")
	}
	if len(astNew.Functions) != 1 {
		t.Fatalf("expected parsed functions in recovered AST")
	}
}

// 5. Concurrent access passes race tests.
func TestCache_ConcurrentAccess_RaceSafety(t *testing.T) {
	c := cache.NewDefault()
	const numGoroutines = 40
	const iterations = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				fileIdx := (gid*iterations + i) % 5 // 5 files shared across goroutines
				relPath := fmt.Sprintf("file_%d.go", fileIdx)
				source := []byte(fmt.Sprintf("package p%d\nfunc F%d() int { return %d }\n", fileIdx, fileIdx, fileIdx))

				_, _, err := c.GetOrParse(context.Background(), "repo-concurrent", relPath, source)
				if err != nil {
					t.Errorf("concurrent GetOrParse failed: %v", err)
					return
				}

				if i%10 == 0 {
					_ = c.Stats()
				}
				if i%25 == 0 && gid == 0 {
					c.InvalidateFile("repo-concurrent", fmt.Sprintf("file_%d.go", (fileIdx+1)%5))
				}
			}
		}(g)
	}

	wg.Wait()

	stats := c.Stats()
	if stats.Hits == 0 {
		t.Fatalf("expected concurrent runs to produce cache hits, got stats: %+v", stats)
	}
}

// 6. Cache-on and cache-off outputs are identical.
func TestCache_CacheOn_CacheOff_OutputsIdentical(t *testing.T) {
	files := map[string][]byte{
		"test.go": []byte(`package sample
import "fmt"
type Greeter struct { Name string }
func (g *Greeter) Greet() string {
    return fmt.Sprintf("Hello, %s", g.Name)
}
`),
		"script.py": []byte(`class MathService:
    def add(self, a, b):
        return a + b
    def multiply(self, a, b):
        return a * b
`),
		"app.ts": []byte(`interface Config { port: number; }
export class Server {
    constructor(private cfg: Config) {}
    start(): void { console.log(this.cfg.port); }
}
`),
	}

	c := cache.NewDefault()

	for filename, source := range files {
		// Run 1: Cache-off (direct ParseAndExtract)
		directAST, err := parser.ParseAndExtract(filename, source)
		if err != nil {
			t.Fatalf("direct parse failed for %s: %v", filename, err)
		}

		// Run 2: Cache-on (cold GetOrParse)
		coldAST, hitCold, err := c.GetOrParse(context.Background(), "repo-cmp", filename, source)
		if err != nil || hitCold {
			t.Fatalf("cold GetOrParse failed for %s: hit=%v, err=%v", filename, hitCold, err)
		}

		// Run 3: Cache-on (warm GetOrParse)
		warmAST, hitWarm, err := c.GetOrParse(context.Background(), "repo-cmp", filename, source)
		if err != nil || !hitWarm {
			t.Fatalf("warm GetOrParse failed for %s: hit=%v, err=%v", filename, hitWarm, err)
		}

		// Compare functions
		if !reflect.DeepEqual(directAST.Functions, coldAST.Functions) {
			t.Fatalf("mismatch between direct and cold functions for %s", filename)
		}
		if !reflect.DeepEqual(directAST.Functions, warmAST.Functions) {
			t.Fatalf("mismatch between direct and warm functions for %s", filename)
		}

		// Compare variables
		if !reflect.DeepEqual(directAST.Variables, coldAST.Variables) {
			t.Fatalf("mismatch between direct and cold variables for %s", filename)
		}
		if !reflect.DeepEqual(directAST.Variables, warmAST.Variables) {
			t.Fatalf("mismatch between direct and warm variables for %s", filename)
		}

		// Compare imports
		if !reflect.DeepEqual(directAST.Imports, coldAST.Imports) {
			t.Fatalf("mismatch between direct and cold imports for %s", filename)
		}

		// Compare diagnostics and metadata
		if !reflect.DeepEqual(directAST.Diagnostics, warmAST.Diagnostics) {
			t.Fatalf("mismatch between direct and warm diagnostics for %s", filename)
		}
		if directAST.Language != warmAST.Language || directAST.ContentHash != warmAST.ContentHash {
			t.Fatalf("metadata mismatch for %s: direct=(%s,%s) warm=(%s,%s)",
				filename, directAST.Language, directAST.ContentHash, warmAST.Language, warmAST.ContentHash)
		}
	}
}

// Cancellation safety: Ensure cancellation does not leave partial entries.
func TestCache_Cancellation_DoesNotLeavePartialEntries(t *testing.T) {
	c := cache.NewDefault()
	source := []byte("package main\nfunc CancelTest() int { return 0 }\n")
	repoID := "repo-cancel"
	relPath := "cancel.go"

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled context

	_, hit, err := c.GetOrParse(ctx, repoID, relPath, source)
	if err == nil {
		t.Fatalf("expected context cancellation error")
	}
	if hit {
		t.Fatalf("should not be a hit on cancelled context")
	}

	// Verify nothing was stored
	stats := c.Stats()
	if stats.Stores != 0 {
		t.Fatalf("expected 0 stores after cancellation, got %d", stats.Stores)
	}

	contentHash := parser.HashSource(source)
	key := cache.Key{
		RepoID:        repoID,
		ContentHash:   contentHash,
		RelPath:       relPath,
		Language:      "go",
		ParserVersion: parser.CurrentParserVersion,
		SchemaVersion: parser.CurrentSchemaVersion,
	}
	if _, found := c.Get(key); found {
		t.Fatalf("cache should not contain entry after cancelled request")
	}
}

// 7. Cold versus warm latency benchmark.
func BenchmarkColdVsWarm(b *testing.B) {
	source := []byte(`
package complexpkg

import "fmt"

type Service struct {
    ID   string
    Name string
}

func NewService(id, name string) *Service {
    return &Service{ID: id, Name: name}
}

func (s *Service) Describe() string {
    return fmt.Sprintf("service %s: %s", s.ID, s.Name)
}

func (s *Service) Validate() error {
    if s.ID == "" {
        return fmt.Errorf("empty ID")
    }
    return nil
}
`)
	c := cache.NewDefault()
	repoID := "bench-repo"
	relPath := "service.go"

	b.Run("Cold_Parse", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			// Direct parse every time
			_, _ = parser.ParseAndExtract(relPath, source)
		}
	})

	// Pre-warm cache
	_, _, _ = c.GetOrParse(context.Background(), repoID, relPath, source)

	b.Run("Warm_CacheHit", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _, _ = c.GetOrParse(context.Background(), repoID, relPath, source)
		}
	})
}
