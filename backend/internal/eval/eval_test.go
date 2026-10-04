package eval

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// fixtureDir points at the shared conflicts/fixtures directory at the repo root.
func fixtureDir() string {
	if v := os.Getenv("FIXTURES_DIR"); v != "" {
		return v
	}
	return filepath.Join("..", "..", "..", "conflicts", "fixtures")
}

func normalize(items []CollisionProjection) []CollisionProjection {
	if items == nil {
		return []CollisionProjection{}
	}
	return items
}

func normalizeGolden(g Golden) Golden {
	g.Collisions = normalize(g.Collisions)
	g.OurChanges = normalize(g.OurChanges)
	g.TheirChanges = normalize(g.TheirChanges)
	return g
}

// TestGoldenFixtures parses every per-language conflict fixture, runs the real
// AST extraction + semantic diff, and asserts the projected collision set
// matches the golden JSON. Run with UPDATE_GOLDEN=1 to regenerate goldens.
func TestGoldenFixtures(t *testing.T) {
	dir := fixtureDir()
	fixturesPath := filepath.Join(dir, "fixtures.json")
	goldenPath := filepath.Join(dir, "golden.json")

	fixtures, err := LoadFixtures(fixturesPath)
	if err != nil {
		t.Fatalf("load fixtures: %v", err)
	}
	if len(fixtures) == 0 {
		t.Fatalf("no fixtures found at %s", fixturesPath)
	}

	update := os.Getenv("UPDATE_GOLDEN") != ""
	goldens, err := LoadGolden(goldenPath)
	if err != nil && !update {
		t.Fatalf("load golden: %v", err)
	}

	var regenerated []Golden
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.Language, func(t *testing.T) {
			got, err := Run(fixture)
			if err != nil {
				t.Fatalf("run fixture %s: %v", fixture.File, err)
			}
			if got.Language != fixture.Language {
				t.Errorf("language = %q, want %q", got.Language, fixture.Language)
			}
			if len(got.Collisions) == 0 {
				t.Errorf("expected at least one structural collision in %s", fixture.File)
			}
			if update {
				regenerated = append(regenerated, normalizeGolden(got))
				return
			}
			want, ok := goldens[fixture.File]
			if !ok {
				t.Fatalf("no golden entry for %s", fixture.File)
			}
			if !reflect.DeepEqual(normalizeGolden(got), normalizeGolden(want)) {
				t.Errorf("golden mismatch for %s\n got: %#v\nwant: %#v", fixture.File, normalizeGolden(got), normalizeGolden(want))
			}
		})
	}

	if update {
		if err := WriteGolden(goldenPath, regenerated); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("regenerated %d golden fixture(s) at %s", len(regenerated), goldenPath)
	}
}
