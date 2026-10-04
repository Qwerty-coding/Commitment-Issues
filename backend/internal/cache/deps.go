package cache

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	parser "CommitIssues/internal/parser"
)

// InvalidateDependents conservatively invalidates every cached entry in repoID
// whose extracted imports reference changedRelPath, without touching
// changedRelPath's own entry. Import strings are matched heuristically (by
// full path, base name and extension-less stem), so a changed file invalidates
// a superset of its true dependents — safe, never stale.
func (c *ASTCache) InvalidateDependents(repoID, changedRelPath string) int {
	clean := normalizeRelPath(changedRelPath)
	tokens := dependencyTokens(clean)
	return c.invalidateEntries(func(entry *Entry) bool {
		if entry.Key.RepoID != repoID {
			return false
		}
		if normalizeRelPath(entry.Key.RelPath) == clean {
			return false
		}
		return importsReference(entry.AST.Imports, tokens)
	})
}

// InvalidateFileDependencyAware invalidates the changed file's own entry and
// every cached entry that imports it. This is the dependency-aware entry point
// to use when a file's content hash changes.
func (c *ASTCache) InvalidateFileDependencyAware(repoID, relPath string) int {
	clean := normalizeRelPath(relPath)
	tokens := dependencyTokens(clean)
	return c.invalidateEntries(func(entry *Entry) bool {
		if entry.Key.RepoID != repoID {
			return false
		}
		if normalizeRelPath(entry.Key.RelPath) == clean {
			return true
		}
		return importsReference(entry.AST.Imports, tokens)
	})
}

// invalidateEntries removes every memory entry matching predicate and its
// corresponding disk payload. It is the shared engine behind all
// content-aware invalidation helpers.
func (c *ASTCache) invalidateEntries(predicate func(*Entry) bool) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	count := 0
	for key, elem := range c.items {
		item := elem.Value.(*lruItem)
		if !predicate(item.entry) {
			continue
		}
		delete(c.items, key)
		c.lru.Remove(elem)
		c.curMemory -= item.entry.SizeBytes
		count++
		if c.config.EnableDisk && c.config.DiskDir != "" {
			_ = os.Remove(filepath.Join(c.config.DiskDir, key+".json"))
		}
	}
	if c.curMemory < 0 {
		c.curMemory = 0
	}
	return count
}

// dependencyTokens returns the candidate substrings used to detect whether an
// import statement references a changed file.
func dependencyTokens(clean string) []string {
	base := path.Base(clean)
	stem := strings.TrimSuffix(base, path.Ext(base))
	dir := path.Dir(clean)
	raw := []string{clean, base, stem, strings.TrimSuffix(clean, path.Ext(clean))}
	if dir != "." && dir != "/" {
		raw = append(raw, path.Join(dir, stem))
	}

	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, tok := range raw {
		tok = strings.TrimSpace(tok)
		if len(tok) < 2 {
			continue
		}
		if _, exists := seen[tok]; exists {
			continue
		}
		seen[tok] = struct{}{}
		out = append(out, tok)
	}
	return out
}

// importsReference reports whether any import statement contains one of the
// dependency tokens. Matching is case-insensitive because import syntax varies
// across languages.
func importsReference(imports []parser.CodeElement, tokens []string) bool {
	for _, imp := range imports {
		text := strings.ToLower(imp.Content)
		if text == "" {
			continue
		}
		for _, tok := range tokens {
			if strings.Contains(text, strings.ToLower(tok)) {
				return true
			}
		}
	}
	return false
}

func normalizeRelPath(p string) string {
	return filepath.ToSlash(filepath.Clean(p))
}
