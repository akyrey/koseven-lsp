# koseven-lsp

A Go LSP server for the [Koseven](https://github.com/koseven/koseven) PHP framework (a PHP 7.4-compatible Kohana 3.3 fork). Works with any LSP-compliant editor; developed primarily for Neovim.

Generic PHP language servers (Intelephense, Psalm) have no knowledge of Koseven's runtime conventions: cascading filesystem view resolution, `View::factory` variable injection, `ORM::factory` type mapping, or HMVC module overrides. This server understands those conventions and provides navigation that generic tools cannot.

## Features

### View navigation

- **Go-to-definition on view names** — cursor on `'pages/about'` inside `View::factory('pages/about')` or `new View('pages/about', ...)` jumps to the resolved `.php` file. Returns all candidates when the same name exists in multiple modules (HMVC cascade).
- **Find-references** — open `application/views/pages/about.php` and request references to see every `View::factory`, `new View`, and `Kohana::find_file('views', ...)` call that constructs it. Also works from a PHP file: cursor on a view name string returns all other call sites. Locations point to the string literal, not the full expression.
- **Inferred view variables** — hover on `$var` inside a view to see its inferred type and originating call sites. `$` completion lists all variables exposed via `->set()`, `->bind()`, the factory second-argument array, `set_global`, and `bind_global`.
- **Hover on view names** — cursor on a view name string shows the resolved file path, cascade order, and module for each candidate.
- **Document symbols** — `:Telescope lsp_document_symbols` (or equivalent) inside a view file shows the view name with all its exposed variables as children, including inferred types.
- **Workspace symbols** — `:Telescope lsp_workspace_symbols` fuzzy-searches all indexed view names.

**Type inference** (inside `->set()` and factory arrays): string/int/float/bool/null literals are typed exactly; `new Foo()` → `Foo`; `ORM::factory('Member')` / `Model::factory('Member')` → `Model_Member`; `Foo::factory('Bar')` → `Foo_Bar`.

**Split-assignment and chained-set support**: all of the following patterns are indexed and variable-tracked:
```php
// Inline chain:
View::factory('pages/about')->set('user', ORM::factory('User'));

// Split assignment:
$view = View::factory('pages/about');
$view->set('user', ORM::factory('User'));
$view->bind('errors', $errors);

// Multi-hop chained sets on a split variable:
$view->set('title', 'Hello')->set('body', 'World')->bind('form', $form);
```
Scope is reset per function/method/closure boundary so variables from one method cannot bleed into another.

### Route and controller navigation

- **`Route::set()->defaults([...])`** — cursor on the `'controller'` value jumps to `classes/Controller/<Name>.php`. Cursor on the `'action'` value jumps directly to the `action_<name>()` method line. The optional `'directory'` key supports HMVC sub-controllers (e.g. `Controller_Admin_Users`).
- **`Route::url('name', [...])`** — same navigation from the params array passed to `url()`.
- **`Request::factory()->controller('x')->action('y')`** — cursor on either string navigates to the controller file or action method.
- **Custom helpers** — configure any static method or global function that takes controller/action as positional string arguments (see [Configuration](#configuration) below).

```php
// All of these support gd on the string arguments:
Route::set('default', '(<controller>(/<action>))')
    ->defaults(['controller' => 'pages', 'action' => 'about']);

Route::url('default', ['controller' => 'pages', 'action' => 'about']);

Request::factory()->controller('pages')->action('about')->execute();

Skp_Helper::getWidget('pages', 'about');  // configured in koseven-ls.toml
```

Cascade-aware: returns all matching locations when the same controller exists in multiple layers.

### Rename and file operations

- **Rename view names** — `grw` (or your editor's rename keybind) on any view name string updates every `View::factory`, `new View`, and `Kohana::find_file('views',...)` call across the project AND renames the physical `.php` view file on disk (when the client supports `workspace/documentChanges` resource operations).
- **File-rename integration** (`workspace/willRenameFiles`) — when you rename a view file via nvim-tree or another LSP-aware file manager, all string references update atomically before the file rename completes.
- **Code action** — a "Rename view '…'" code action appears on any view name string, triggering the editor's built-in rename workflow.

### Other navigation

- **`ORM::factory('Member')` / `Model::factory('Member')`** → jumps to `classes/Model/Member.php` in the cascade. Compound names work: `ORM::factory('Member_Profile')` → `classes/Model/Member/Profile.php`.
- **`Kohana::find_file('classes', 'Model_Member')`** → jumps to the class file in the cascade. Also works for `'i18n'`, `'messages'`, `'config'`, and `'media'`.
- **`Kohana::message('file', ...)`** → jumps to `messages/file.php` in the cascade.

### Diagnostics

- **Missing-view diagnostic** *(opt-in)* — set `diagnostics.missing_views = true` in `koseven-ls.toml` to get a Warning when `View::factory('name')` resolves to no file. Off by default to avoid noise during renames and file moves.

**Cascade awareness**: reads `application/bootstrap.php` to discover enabled modules and their load order. `Kohana::modules([...])` must be a static array literal (dynamic/conditional loading is not supported).

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

# Register custom functions/static methods that take controller and action as
# positional string arguments, enabling go-to-definition from those call sites.
# Use one [[route_helpers]] block per function. Fields:
#   name       = "ClassName::method"  or just "function_name" for global functions
#   controller = 0-based arg index of the controller string (omit if not present)
#   action     = 0-based arg index of the action string    (omit if not present)
#   directory  = 0-based arg index of the HMVC directory   (omit if not present)
#
# Example — Skp_Helper::getWidget(string $controller, string $action, ...):
[[route_helpers]]
name       = "Skp_Helper::getWidget"
controller = 0
action     = 1

# Additional helpers — repeat the block for each function:
# [[route_helpers]]
# name       = "My_Helper::renderPartial"
# controller = 0
# action     = 1
# directory  = 2
```

### Cache

The index is persisted to `.cache/koseven-ls/index.gob` in the project root after each full walk. Subsequent server starts load from this cache and skip re-parsing when no files have changed. Add `.cache/koseven-ls/` to your `.gitignore`.

## Project layout

```
cmd/koseven-lsp/    entry point
internal/
  config/           koseven-ls.toml loader
  indexer/view/     view index: Walk, ReindexFile, extract, types, Index interface
  lsp/              LSP server, definition, references, hover, completion, handlers
  phpparse/         VKCOM php-parser wrapper
  phputil/          AST helpers (ArgExpr, ScalarStringVal, FQN, Location)
  project/          bootstrap.php reader, cascade roots, module types
testdata/
  stock/            minimal Koseven fixture (no modules)
  hmvc/             HMVC fixture with cascading module overrides
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

**Next**
- `Kohana::message` completion — suggest message keys from `messages/*.php` files
- `__('key')` i18n completion
- `Kohana::$config->load('group.key')` completion and navigation
- Diagnostic for unused `->set()` vars (opt-in)
- `Route::url()` completion — suggest registered route names

## License

MIT
