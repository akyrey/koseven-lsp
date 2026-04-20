# CLAUDE.md

Project context for Claude Code sessions.

## What is this project?

`koseven-lsp` is a Go LSP server for the Koseven PHP framework (a PHP 7.4-compatible
Kohana 3.3 fork). It provides go-to-definition, find-references, and inferred
variable hover/completion for Koseven-specific conventions that generic PHP
language servers (Intelephense, Psalm) have no knowledge of: the cascading
filesystem view resolver, `View::factory` variable injection, `ORM::factory` type
mapping, and HMVC module overrides.

The architecture mirrors `~/personal/laravel-ls`: same PHP parser (VKCOM),
same LSP framework (tliron/glsp), same atomic-swap index pattern, same debounced
fsnotify watcher. Refer to that project when you need a working example of a
pattern not yet implemented here.

## Tech stack

- **Language**: Go 1.23+
- **PHP parser**: `github.com/VKCOM/php-parser` v0.8.2 (pure Go, no CGO, PHP 8.1 config)
- **LSP framework**: `github.com/tliron/glsp` v0.2.2, protocol 3.16, stdio transport
- **File watcher**: `github.com/fsnotify/fsnotify` — 500 ms debounce, watches application/ and modules/
- **Config**: `github.com/BurntSushi/toml` — reads `koseven-ls.toml` from project root
- **Build**: `make build` (outputs `./koseven-lsp`) or `go build -o koseven-lsp ./cmd/koseven-lsp`
- **Tests**: `make test` / `go test ./...`; always run `make test-race` before committing

## Commands

```bash
make build        # build ./koseven-lsp
make test         # go test ./... -count=1
make test-race    # go test -race ./... -count=1
make vet          # go vet ./...
make fmt          # gofmt -s -w .
make tidy         # go mod tidy && go mod verify
make lint         # golangci-lint run ./... (requires golangci-lint)
make install      # go install ./cmd/koseven-lsp
```

## Project layout

```
cmd/koseven-lsp/main.go             # entry point only — no logic here
internal/
  config/
    config.go                       # Config struct + Load() + Defaults()
  indexer/view/
    types.go                        # ViewDefinition, ViewUsage, ExposedVar, PHPType, Index iface
    index.go                        # ViewIndex — concrete Index implementation + withoutFile()
    walk.go                         # Walk() + ReindexFile() + discoverViews + extractDir
    extract.go                      # extractVisitor + tryExtractChain + inferType
  lsp/
    server.go                       # Server struct — cascadeState(), reindex(), reindexFiles(), watcher
    definition.go                   # textDocument/definition — view, ORM::factory, Kohana::find_file
    references.go                   # textDocument/references — view-file mode + string mode
    hover.go                        # textDocument/hover — view hover + $var type hover
    completion.go                   # textDocument/completion — $var list in view files
    diagnostics.go                  # textDocument/publishDiagnostics — missing-view opt-in
    symbols.go                      # workspace/symbol — view name fuzzy search
    rename.go                       # textDocument/rename + prepareRename + workspace/willRenameFiles
    handlers.go                     # textDocument/documentSymbol (stub)
    documents.go                    # DocumentStore — in-memory cache with disk fallback
    uri.go                          # URIToPath, PathToURI, toLSPLocation, UTF-16 column math
  phpparse/
    parse.go                        # Bytes() + File() — shared PHP 8.1 parse helpers
  phputil/
    fqn.go                          # FQN type, UseMap, FileContext + Resolve()
    ast.go                          # ArgExpr, ScalarStringVal, NameToString, ClassName, etc.
    location.go                     # Location type + FromPosition()
  project/
    module.go                       # RootKind, Module, ViewRoot types
    bootstrap.go                    # ParseModules() — static Kohana::modules([...]) extraction
    roots.go                        # BuildViewRoots(), PHPScanDirs()
    cascade.go                      # CascadeBase, BuildCascadeBases(), FindCascadeFiles(), ClassNameToRelPath()
testdata/
  stock/                            # minimal Koseven: no modules, one view, one controller
  hmvc/                             # HMVC: modules/blog/ + modules/auth/ with cascade overlap
```

## Koseven domain knowledge

### Cascading filesystem

Koseven resolves files in priority order: `application/` → modules (in the order
listed in `Kohana::modules([...])` in `application/bootstrap.php`) → `system/`.
The first match wins. `View::factory('pages/about')` resolves to the first
`views/pages/about.php` found in that cascade.

This means a module can override any application or system view by providing its
own file at the same path. HMVC projects use this heavily. The index must return
**all** candidates when a name resolves to multiple layers — LSP `definition`
accepts an array of `Location`.

### View variable injection APIs

All of these expose variables inside the view's `extract()`-ed scope:

| API | Notes |
|-----|-------|
| `View::factory('name', ['x' => $val])` | second arg; also `new View('name', [...])` |
| `->set('x', $val)` | single var |
| `->set(['x' => $val, 'y' => $val2])` | batch |
| `->bind('x', $ref)` | by reference |
| `$view->x = $val` | magic `__set` |
| `View::set_global('x', $val)` | available in every view |
| `View::bind_global('x', $ref)` | available in every view |

The user's fork has **no custom exposure APIs** — stock semantics hold.

### Module loading

`Kohana::modules([...])` in `application/bootstrap.php` is always a static array
literal in this project (no dynamic/conditional loading). Bootstrap extraction
can assume a single static call site.

### ORM naming convention

`ORM::factory('Member')` resolves to `Model_Member` (underscore-prefix PSR-0
style). Subtypes follow the same pattern: `ORM::factory('Member_Profile')` →
`Model_Member_Profile`. Class files live at `classes/Model/Member.php` and
`classes/Model/Member/Profile.php` respectively (Kohana's PSR-0 path mapping).

## Architecture

**Index contract**: `internal/indexer/view.Index` is the single interface all
three MVP LSP features read from. Indexer packages are **pure** — they take a
root path and return an index value. No LSP protocol types leak into them.
The `lsp` package is the only layer that touches `tliron/glsp` types.

**Visitor pattern**: embed `visitor.Null` (VKCOM's no-op visitor), override only
the methods needed. The traverser recurses into children automatically — no
manual child traversal. See `~/personal/laravel-ls/internal/indexer/eloquent/`
for a complete working example.

**Atomic index swap**: the `Server` holds the current `view.Index` behind an
`sync.RWMutex`. `reindex()` builds a new index in a goroutine and swaps it in
under a write lock. Handlers take a read lock for the duration of the request.
Per-file incremental reindex returns a new `*ViewIndex` pointer; the server
swaps it atomically. Falls back to full reindex when the symbol table is absent.

**FileContext**: built incrementally during AST traversal. PHP namespace and
`use` statements appear before class/function bodies, so `fc.Namespace` and
`fc.Uses` are always populated by the time extraction methods fire.

**VKCOM parser conventions**:
- `EndPos` is exclusive (one past last byte), matching LSP's exclusive range end.
  Use it directly in `toLSPRange` without adding 1.
- `ExprVariable.Name` for `$this` has `Identifier.Value == "$this"` (dollar sign
  included). All variable name comparisons must include `$`.
- `*ast.NameFullyQualified` nodes return `"\Foo\Bar"` (leading backslash) from
  `NameToString`. Always call `fc.Resolve()` after `NameToString` to strip it.

## Testing conventions

- External test packages (`package view_test`) for black-box tests.
- Fixtures live in `testdata/` at repo root. Reference by relative path from the
  test file, e.g. `../../../testdata/stock`.
- Table-driven tests for all multi-case scenarios.
- Race detector must pass: `make test-race`.
- Test the index/analysis layer directly (not through LSP transport) for fast
  iteration. The `testdata/stock` and `testdata/hmvc` fixtures cover the two
  main scenarios: no-module project and HMVC with cascading overrides.

## Key design decisions

- **VKCOM/php-parser over tree-sitter**: pure Go (no CGO), full PHP AST,
  better suited to type inference on `->set()` RHS values and `ORM::factory`
  class name resolution. Matches the stack used in `~/personal/laravel-ls`.
- **tliron/glsp over go.lsp.dev**: higher-level handler framework, matches
  laravel-ls so patterns transfer directly.
- **All three MVP features share one index**: `viewName → []ViewDefinition`
  (for go-to-def), `viewName → []ViewUsage` (for find-refs + var inference).
  Building the index once lights up all three features.
- **Missing-views diagnostic is opt-in**: `diagnostics.missing_views = false`
  by default to avoid noise during file renames and moves.
- **set_global/bind_global vars always surface in completion**: they are part of
  every view's effective scope. They are not flagged as "unset" in diagnostics.

## Architecture details

**Chain extraction deduplication**: `tryExtractChain` is called from `ExprStaticCall`,
`ExprNew`, and `ExprMethodCall` visitors. DFS pre-order visits the outermost method
call first. The first call that resolves to a view construction records the full
chain in `seen[basePos]`; inner nodes see the key and skip. This means chained
constructions (`View::factory()->set()->bind()`) are captured correctly.

**Split-assignment scope tracking** (implemented in `extractVisitor`):
- `ExprAssign` fires before its children (DFS pre-order). When the RHS is a view
  construction, the usage is recorded immediately (marking `basePos` as `seen`) and
  the LHS variable name is stored in `scope[varName] = usageIdx`.
- `ExprMethodCall.tryScopeAttribution` fires for every method call. If the direct
  receiver is an `ExprVariable` tracked in `scope`, it appends vars from `set/bind`
  to the stored usage — no `tryExtractChain` needed.
- Scope is cleared (`StmtClassMethod`, `StmtFunction`, `ExprClosure`,
  `ExprArrowFunction`) so method variables don't bleed across boundaries.
- `exprVariableName` normalises variable names to always include `$` prefix,
  handling both `"$view"` and `"view"` from VKCOM (depending on parser version).
- **Limitation**: `$view->set('a')->set('b')` only captures `'a'` (direct receiver),
  not `'b'` (chained on the result). Multi-hop scope chains are deferred.

**`ExprStaticCall.Call` vs `.Method`**: VKCOM parser uses `Call` (not `Method`) for
the method name field of static calls (`View::factory`). Instance method calls
(`ExprMethodCall`) use `Method`. This asymmetry has burned us once — guard against it.

**`NameRange` vs `Range` in ViewUsage**: `Range` spans the full construction expression
(for watcher/index tracking). `NameRange` spans only the string literal (for references
and diagnostics shown in the editor). Use `usageRange(u)` in `lsp/` code, which prefers
`NameRange` when non-zero. `NameRange` is line-level only (Character: 0) because the
extractor doesn't have source bytes available. Full column precision would require
threading `src []byte` through `extractFileUsages`.

**Diagnostics are opt-in**: `diagnostics.missing_views` defaults to `false`. Pushed on
`DidOpen`, `DidChange`, and cleared (empty list) on `DidClose`. Background reindex does
not push diagnostics; they refresh on next file open/edit.

**`RootKind` lives in `project` package**: `view/types.go` re-exports the constants
as aliases to avoid a circular import (`view → project` is fine; `project → view` is
not since `view/walk.go` imports `project`).

**VKCOM `StartPos` is 0-indexed**: matches `bytes.Index` byte offsets exactly. Any
assertion comparing them must NOT subtract 1. Confirmed by test in `rename_test.go`.

**`workspace/willRenameFiles` requires client support**: nvim-tree and oil.nvim send
this notification; plain `:!mv` or `:e` do not. When the client does send it, the
handler calls `NamesForFile(oldPath)` on the view index and `viewNameFromPath(newPath)`
against the live view roots. If either name is empty (path outside view roots, or
index not yet built), the file's edits are skipped safely.

**`viewNameFromPath`** uses `filepath.Rel` against each view root in cascade order.
The first successful relative path (no `..` prefix) wins. This is the inverse of
`discoverViews` in `walk.go`, which uses the same logic in forward direction.

**Rename only updates string literals, not the file**: `textDocument/rename` replaces
every view name string literal (including quotes, preserving single/double quote style)
across all usage files. It does NOT rename the `.php` view file on disk — that requires
either `workspace/willRenameFiles` (planned) or the developer renaming the file manually.

**Cascade go-to-def uses stat-checks, not an index**: `ORM::factory` and
`Kohana::find_file` (non-view) resolve files with `project.FindCascadeFiles` — a
sequence of `os.Stat` calls against each cascade base. This is fast enough (3–10
checks per request) and requires no additional index. It means go-to-def works even
before the view index finishes building.

**`ClassNameToRelPath`**: replaces `_` with `/` and appends `.php`. Kohana's PSR-0
class-to-path convention. `Model_Member` → `Model/Member.php`, not lowercase.
Kohana's autoloader lowercases on-the-fly but file names match the class name case.

**`withoutFile` shares definition maps**: `ViewIndex.withoutFile(path)` returns a
new index where `byName`/`byPath` are shared (definitions), only `usages` is
filtered. This is safe because `ReindexFile` only updates usages; view file
creation/deletion requires a full `Walk`.

## Open questions (to answer before next iteration)

1. Is `application/bootstrap.php` always at that exact path, or should
   `application_path` in config override the bootstrap location too?
2. Does the debugbar-augmented `Kohana` class preserve the static-array call
   shape of `modules()`? (Affects whether static AST extraction of the module
   list works unchanged.)
3. Case sensitivity of view names — does the project rely on case-insensitive
   resolution (e.g. `pages/About` → `pages/about.php`)?
4. Should the index walk respect `.gitignore`, or walk everything under the
   known cascade roots only?
5. Cache location — `.cache/koseven-ls/` in project root, or XDG cache dir?
6. Variable scope tracking priority — how common is the split-assignment pattern
   (`$view = View::factory(...); $view->set(...)`) in the real codebase?
