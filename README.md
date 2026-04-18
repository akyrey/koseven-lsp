# koseven-lsp

A Go LSP server for the [Koseven](https://github.com/koseven/koseven) PHP framework (a PHP 7.4-compatible Kohana 3.3 fork). Works with any LSP-compliant editor; developed primarily for Neovim.

Generic PHP language servers (Intelephense, Psalm) have no knowledge of Koseven's runtime conventions: cascading filesystem view resolution, `View::factory` variable injection, `ORM::factory` type mapping, or HMVC module overrides. This server understands those conventions and provides navigation that generic tools cannot.

## Features (v0.1.0 — in progress)

- **Go-to-definition on view names** — cursor on `'pages/about'` inside `View::factory('pages/about')` or `new View('pages/about', ...)` jumps to the resolved `.php` file. Returns all candidates when the same name exists in multiple modules (HMVC cascade).
- **Find-references from a view file** — open `application/views/pages/about.php` and request references to see every `View::factory`, `new View`, and `Kohana::find_file('views', ...)` call that constructs it.
- **Inferred view variables** — hover on a bare `$var` inside a view to see its inferred type and which call sites set it; `$` completion lists all variables exposed via `->set()`, `->bind()`, the factory array, magic `__set`, and `set_global`/`bind_global`.

## Installation

```bash
go install github.com/akyrey/koseven-lsp/cmd/koseven-lsp@latest
```

Or build from source:

```bash
git clone https://github.com/akyrey/koseven-lsp
cd koseven-lsp
make build        # outputs ./koseven-lsp
make install      # installs to $GOPATH/bin
```

## Neovim setup

```lua
-- Using nvim-lspconfig (add to your config):
local lspconfig = require('lspconfig')
local configs = require('lspconfig.configs')

if not configs.koseven_lsp then
  configs.koseven_lsp = {
    default_config = {
      cmd = { 'koseven-lsp' },
      filetypes = { 'php' },
      root_dir = lspconfig.util.root_pattern(
        'application/bootstrap.php',
        'koseven-ls.toml'
      ),
      single_file_support = false,
    },
  }
end

lspconfig.koseven_lsp.setup({})
```

## Configuration

Drop a `koseven-ls.toml` in your project root to override defaults. All fields are optional — zero-config works for a stock Koseven layout.

```toml
# koseven-ls.toml

application_path = "application"  # default
system_path      = "system"       # default
modules_path     = "modules"      # default

# Additional view roots outside the standard cascade:
extra_view_roots = []

[diagnostics]
# Emit a diagnostic when View::factory('x') resolves to no file.
# Disabled by default to avoid noise during file moves / renames.
missing_views = false
```

## Project layout

```
cmd/koseven-lsp/    entry point
internal/
  config/           koseven-ls.toml loader
  indexer/view/     view index: definitions, usages, exposed vars
  lsp/              LSP server, handlers, document store, URI helpers
  phpparse/         VKCOM php-parser wrapper
  phputil/          AST helpers, FQN resolution, location types
  project/          bootstrap.php reader, cascade root builder
testdata/
  stock/            minimal Koseven fixture (no modules)
  hmvc/             HMVC fixture with overlapping view names across modules
```

## Development

```bash
make build        # go build -o koseven-lsp ./cmd/koseven-lsp
make test         # go test ./... -count=1
make test-race    # go test -race ./... -count=1
make vet          # go vet ./...
make fmt          # gofmt -s -w .
make tidy         # go mod tidy && go mod verify
make lint         # golangci-lint run ./...  (requires golangci-lint)
```

Always run `make test-race` before committing.

## Tech stack

| Concern | Choice |
|---------|--------|
| PHP parsing | [`github.com/VKCOM/php-parser`](https://github.com/VKCOM/php-parser) — pure Go, PHP 8.1, no CGO |
| LSP wiring | [`github.com/tliron/glsp`](https://github.com/tliron/glsp) — protocol 3.16, stdio transport |
| File watching | [`github.com/fsnotify/fsnotify`](https://github.com/fsnotify/fsnotify) — debounced reindex |
| Config | [`github.com/BurntSushi/toml`](https://github.com/BurntSushi/toml) |

## Roadmap

**v0.2.x**
- `ORM::factory('Member')` → `classes/Model/Member.php` go-to-definition
- `Kohana::find_file('classes'|'i18n'|'messages', ...)` go-to-definition
- Code action: insert `->set('foo', null)` for an unset view variable

**v0.3.x**
- `Kohana::message('file.key')` completion and navigation
- `__('key')` i18n completion
- `Kohana::$config->load('group.key')` completion
- Diagnostic for unused `->set()` vars (opt-in)

## License

MIT
