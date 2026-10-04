package cache

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Cache configuration environment variables. Configuration precedence is:
//
//	explicit Config fields > environment variables > defaults.
const (
	// EnvDiskEnabled toggles the persistent on-disk cache ("true"/"1"/"yes").
	EnvDiskEnabled = "CACHE_DISK_ENABLED"
	// EnvDiskDir is the directory used for persistent cache files.
	EnvDiskDir = "CACHE_DIR"
	// EnvMaxDiskBytes caps total on-disk cache size in bytes.
	EnvMaxDiskBytes = "CACHE_MAX_DISK_BYTES"
	// EnvMaxEntries caps the number of in-memory LRU entries.
	EnvMaxEntries = "CACHE_MAX_ENTRIES"
	// EnvMaxMemoryBytes caps in-memory cache size in bytes.
	EnvMaxMemoryBytes = "CACHE_MAX_MEMORY_BYTES"
)

// ConfigFromEnv builds a cache Config from environment variables over the safe
// defaults. Enabling the disk cache without an explicit directory falls back to
// a deterministic per-user cache directory so the feature never writes to an
// unexpected location.
func ConfigFromEnv() Config {
	cfg := DefaultConfig()

	if v := strings.TrimSpace(os.Getenv(EnvDiskEnabled)); v != "" {
		cfg.EnableDisk = parseBool(v)
	}
	if v := strings.TrimSpace(os.Getenv(EnvDiskDir)); v != "" {
		cfg.DiskDir = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvMaxDiskBytes)); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			cfg.MaxDiskBytes = n
		}
	}
	if v := strings.TrimSpace(os.Getenv(EnvMaxEntries)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxEntries = n
		}
	}
	if v := strings.TrimSpace(os.Getenv(EnvMaxMemoryBytes)); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			cfg.MaxMemoryBytes = n
		}
	}

	if cfg.EnableDisk && strings.TrimSpace(cfg.DiskDir) == "" {
		if dir, err := os.UserCacheDir(); err == nil && dir != "" {
			cfg.DiskDir = filepath.Join(dir, "commitissues", "ast")
		}
	}
	return cfg
}

// NewFromEnv creates a cache from ConfigFromEnv, falling back to an in-memory
// default when the environment specifies an unusable disk directory.
func NewFromEnv() (*ASTCache, error) {
	return New(ConfigFromEnv())
}

func parseBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on", "enabled":
		return true
	default:
		return false
	}
}
