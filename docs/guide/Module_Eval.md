# Module: Eval

## Purpose

`backend/internal/eval` is a golden-fixture harness for multi-language AST
extraction. It loads per-language conflict fixtures (base/ours/theirs
source triplets), runs the real parser + semantic diff, and projects the
result to a stable, line-independent shape that is compared against
golden JSON. Its job is regression safety for the parser/semantic stack:
any change that alters which symbols are detected shows up as a fixture
diff instead of a silent behavior change.

## Files

### `backend/internal/eval/eval.go`

#### Primary Role
Loads the fixture manifest, executes the real pipeline per fixture, and
produces `Golden` results plus the golden-file update path.

#### Key Structures
- `CollisionProjection{Type, Kind, Name}` — the stable projection of a
  `DiffItem`. Line numbers, file paths and full code content are
  intentionally excluded so fixtures stay robust to incidental
  formatting changes while still pinning the exact symbol identity of
  every collision.
- `Fixture{Language, File, Base, Ours, Theirs}` — one per-language
  conflict fixture: three complete source versions of the same file.
- `Golden{Language, File, Collisions, OurChanges, TheirChanges}` — the
  expected output of running a `Fixture` through the parser and
  semantic diff.
- `LoadFixtures(path) ([]Fixture, error)` — reads the fixture manifest
  (a JSON array of `Fixture`).
- `Run(f Fixture) (Golden, error)` — parses base/ours/theirs with
  `internal/parser`, diffs with `internal/semantic`, and projects every
  `DiffItem` to `CollisionProjection`.

#### Wiring (Dependencies)
- Imports: `internal/parser`, `internal/semantic`, stdlib
  (`context`, `encoding/json`, `fmt`, `os`, `sort`).
- Imported by: `internal/eval` tests and `internal/bench` (the metrics
  harness reuses the fixtures as representative payloads).

## Fixture layout

Fixtures live in the shared repo-root directory `conflicts/fixtures/`:

- `conflicts/fixtures/fixtures.json` — the manifest (JSON array of
  `Fixture`; each entry names the language and carries the three source
  versions inline).
- `conflicts/fixtures/golden.json` — the expected `Golden` output per
  fixture, keyed in manifest order.

## Tests

- `backend/internal/eval/eval_test.go` — `TestGoldenFixtures` runs every
  fixture through the real parser + semantic diff and asserts the
  projected result matches `golden.json` exactly.

## Regenerating goldens

Run with `UPDATE_GOLDEN=1` to regenerate the golden file after an
intentional behavior change:

```bash
cd backend && UPDATE_GOLDEN=1 go test ./internal/eval/
```

Then review the `golden.json` diff carefully — it is the contract. All
other JSON contracts (HTTP API, cache persistence, suggestions/
resolutions DTOs) are unaffected by the projection.
