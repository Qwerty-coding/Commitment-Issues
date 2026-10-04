package cache_test

import (
	"context"
	"testing"

	"CommitIssues/internal/cache"
	"CommitIssues/internal/parser"
)

func cacheKey(repoID, relPath string, source []byte) cache.Key {
	return cache.Key{
		RepoID:        repoID,
		ContentHash:   parser.HashSource(source),
		RelPath:       relPath,
		Language:      parser.GetLanguageName(relPath),
		ParserVersion: parser.CurrentParserVersion,
		SchemaVersion: parser.CurrentSchemaVersion,
	}
}

func TestCache_InvalidateDependents(t *testing.T) {
	c := cache.NewDefault()
	repo := "repo-deps"

	app := []byte("from core.cache import RedisCacheManager\n\ndef use():\n    return RedisCacheManager\n")
	core := []byte("class RedisCacheManager:\n    pass\n")
	unrelated := []byte("def helper():\n    return 1\n")

	for path, src := range map[string][]byte{
		"app.py":        app,
		"core/cache.py": core,
		"unrelated.py":  unrelated,
	} {
		if _, _, err := c.GetOrParse(context.Background(), repo, path, src); err != nil {
			t.Fatalf("seed %s: %v", path, err)
		}
		if _, hit := c.Get(cacheKey(repo, path, src)); !hit {
			t.Fatalf("expected %s to be cached", path)
		}
	}

	// Changing core/cache.py must invalidate app.py (it imports core.cache) but
	// leave unrelated.py and core/cache.py's own entry alone.
	if got := c.InvalidateDependents(repo, "core/cache.py"); got != 1 {
		t.Fatalf("InvalidateDependents removed %d, want 1", got)
	}
	if _, hit := c.Get(cacheKey(repo, "app.py", app)); hit {
		t.Error("app.py should have been invalidated as a dependent")
	}
	if _, hit := c.Get(cacheKey(repo, "core/cache.py", core)); !hit {
		t.Error("changed file's own entry should be untouched by InvalidateDependents")
	}
	if _, hit := c.Get(cacheKey(repo, "unrelated.py", unrelated)); !hit {
		t.Error("unrelated.py must not be invalidated")
	}
}

func TestCache_InvalidateFileDependencyAware(t *testing.T) {
	c := cache.NewDefault()
	repo := "repo-deps-aware"

	app := []byte("import core.cache\n")
	core := []byte("VALUE = 1\n")

	_, _, _ = c.GetOrParse(context.Background(), repo, "app.py", app)
	_, _, _ = c.GetOrParse(context.Background(), repo, "core/cache.py", core)

	if got := c.InvalidateFileDependencyAware(repo, "core/cache.py"); got != 2 {
		t.Fatalf("InvalidateFileDependencyAware removed %d, want 2 (file + dependent)", got)
	}
	if _, hit := c.Get(cacheKey(repo, "core/cache.py", core)); hit {
		t.Error("changed file should be invalidated")
	}
	if _, hit := c.Get(cacheKey(repo, "app.py", app)); hit {
		t.Error("dependent should be invalidated")
	}
}

func TestCacheConfigFromEnv(t *testing.T) {
	t.Setenv(cache.EnvDiskEnabled, "true")
	t.Setenv(cache.EnvDiskDir, "/tmp/commitissues-cache-test")
	t.Setenv(cache.EnvMaxDiskBytes, "2048")
	t.Setenv(cache.EnvMaxEntries, "42")
	t.Setenv(cache.EnvMaxMemoryBytes, "4096")

	cfg := cache.ConfigFromEnv()
	if !cfg.EnableDisk {
		t.Error("EnableDisk should be true")
	}
	if cfg.DiskDir != "/tmp/commitissues-cache-test" {
		t.Errorf("DiskDir = %q", cfg.DiskDir)
	}
	if cfg.MaxDiskBytes != 2048 {
		t.Errorf("MaxDiskBytes = %d, want 2048", cfg.MaxDiskBytes)
	}
	if cfg.MaxEntries != 42 {
		t.Errorf("MaxEntries = %d, want 42", cfg.MaxEntries)
	}
	if cfg.MaxMemoryBytes != 4096 {
		t.Errorf("MaxMemoryBytes = %d, want 4096", cfg.MaxMemoryBytes)
	}
}

func TestCacheConfigFromEnv_DisabledByDefault(t *testing.T) {
	for _, env := range []string{cache.EnvDiskEnabled, cache.EnvDiskDir, cache.EnvMaxDiskBytes} {
		t.Setenv(env, "")
	}
	cfg := cache.ConfigFromEnv()
	if cfg.EnableDisk {
		t.Error("disk cache should be disabled by default")
	}
}
