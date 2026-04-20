package lsp

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/tliron/commonlog"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/akyrey/koseven-lsp/internal/config"
	"github.com/akyrey/koseven-lsp/internal/indexer/view"
	"github.com/akyrey/koseven-lsp/internal/project"
)

// reindexDebounce is how long to wait after the last PHP file change before
// triggering a reindex.
const reindexDebounce = 500 * time.Millisecond

// watchExts are file extensions that trigger reindexing on change.
var watchExts = []string{".php"}

// Server holds all LSP state: open document cache, view index, and project root.
// Handler methods are registered in main.go.
type Server struct {
	version string
	log     commonlog.Logger

	mu       sync.RWMutex
	root     string
	cfg      config.Config
	modules  []project.Module // populated after bootstrap.php is parsed
	scanOnce sync.Once
	viewIdx  view.Index

	docs *DocumentStore
}

// NewServer creates a Server ready to accept LSP requests.
func NewServer(log commonlog.Logger, version string) *Server {
	return &Server{
		version: version,
		log:     log,
		docs:    newDocumentStore(),
	}
}

// Initialize detects the project root, loads config, and returns server capabilities.
func (s *Server) Initialize(_ *glsp.Context, p *protocol.InitializeParams) (any, error) {
	root := detectRoot(p)
	cfg, err := config.Load(root)
	if err != nil {
		s.log.Warningf("koseven-lsp: config load: %v", err)
		cfg = config.Defaults()
	}

	s.mu.Lock()
	s.root = root
	s.cfg = cfg
	s.mu.Unlock()
	s.log.Infof("koseven-lsp: root=%s", root)

	syncKind := protocol.TextDocumentSyncKindFull
	ver := s.version
	return protocol.InitializeResult{
		Capabilities: protocol.ServerCapabilities{
			TextDocumentSync:       syncKind,
			DefinitionProvider:     true,
			ReferencesProvider:     true,
			HoverProvider:          true,
			CompletionProvider:     &protocol.CompletionOptions{},
			DocumentSymbolProvider: true,
			WorkspaceSymbolProvider: true,
		},
		ServerInfo: &protocol.InitializeResultServerInfo{
			Name:    "koseven-lsp",
			Version: &ver,
		},
	}, nil
}

// Initialized kicks off background indexing then starts the file-watcher.
func (s *Server) Initialized(_ *glsp.Context, _ *protocol.InitializedParams) error {
	s.scanOnce.Do(func() {
		go func() {
			s.mu.RLock()
			root, cfg := s.root, s.cfg
			s.mu.RUnlock()
			if root == "" {
				return
			}
			s.reindex(root, cfg)
			s.startWatcher(root)
		}()
	})
	return nil
}

// DidOpen caches the newly opened document and pushes diagnostics.
func (s *Server) DidOpen(ctx *glsp.Context, p *protocol.DidOpenTextDocumentParams) error {
	src := []byte(p.TextDocument.Text)
	s.docs.Set(p.TextDocument.URI, src)
	s.mu.RLock()
	idx, cfg := s.viewIdx, s.cfg
	s.mu.RUnlock()
	path := URIToPath(p.TextDocument.URI)
	publishViewDiagnostics(ctx, p.TextDocument.URI, src, path, idx, cfg)
	return nil
}

// DidChange updates the cached document content and pushes diagnostics.
// Full sync: uses first change.
func (s *Server) DidChange(ctx *glsp.Context, p *protocol.DidChangeTextDocumentParams) error {
	if len(p.ContentChanges) == 0 {
		return nil
	}
	var src []byte
	switch c := p.ContentChanges[0].(type) {
	case protocol.TextDocumentContentChangeEventWhole:
		src = []byte(c.Text)
	case protocol.TextDocumentContentChangeEvent:
		src = []byte(c.Text)
	}
	if src == nil {
		return nil
	}
	s.docs.Set(p.TextDocument.URI, src)
	s.mu.RLock()
	idx, cfg := s.viewIdx, s.cfg
	s.mu.RUnlock()
	path := URIToPath(p.TextDocument.URI)
	publishViewDiagnostics(ctx, p.TextDocument.URI, src, path, idx, cfg)
	return nil
}

// DidClose removes the document from the cache and clears its diagnostics.
func (s *Server) DidClose(ctx *glsp.Context, p *protocol.DidCloseTextDocumentParams) error {
	s.docs.Delete(p.TextDocument.URI)
	// Clear diagnostics so stale warnings don't persist after closing.
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()
	publishViewDiagnostics(ctx, p.TextDocument.URI, nil, "", nil, cfg)
	return nil
}

// Shutdown is a no-op; cleanup happens at process exit.
func (s *Server) Shutdown(_ *glsp.Context) error { return nil }

// SetTrace is a no-op.
func (s *Server) SetTrace(_ *glsp.Context, _ *protocol.SetTraceParams) error { return nil }

// viewIndex returns the current index under a read lock.
// Returns nil when indexing has not completed yet.
func (s *Server) viewIndex() view.Index {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.viewIdx
}

// cascadeState returns the project root, config, and module list needed for
// cascade file lookups. Safe to call from any handler goroutine.
func (s *Server) cascadeState() (root string, cfg config.Config, modules []project.Module) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.root, s.cfg, s.modules
}

// reindex rebuilds the view index and atomically swaps it in.
func (s *Server) reindex(root string, cfg config.Config) {
	s.log.Infof("koseven-lsp: indexing %s", root)

	modules, err := project.ParseModules(root, cfg)
	if err != nil {
		s.log.Warningf("koseven-lsp: bootstrap parse: %v (continuing with no modules)", err)
		modules = nil
	}
	s.log.Infof("koseven-lsp: found %d module(s)", len(modules))

	idx, err := view.Walk(root, cfg, modules)
	if err != nil {
		s.log.Errorf("koseven-lsp: view walk: %v", err)
		return
	}
	s.log.Infof("koseven-lsp: indexed %d view definition(s)", len(idx.AllDefinitions()))

	s.mu.Lock()
	s.modules = modules
	s.viewIdx = idx
	s.mu.Unlock()
	s.log.Infof("koseven-lsp: indexing complete")
}

// startWatcher watches PHP files under the project for changes outside the
// editor (e.g., git checkout, code generation) and triggers debounced reindexing.
func (s *Server) startWatcher(root string) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		s.log.Errorf("koseven-lsp: watcher init: %v", err)
		return
	}

	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()

	watchRoots := []string{
		filepath.Join(root, cfg.ApplicationPath),
		filepath.Join(root, cfg.ModulesPath),
	}
	for _, dir := range watchRoots {
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil
			}
			return w.Add(path)
		})
	}

	go s.watchLoop(w, root)
}

// watchLoop is the fsnotify event loop with debouncing.
func (s *Server) watchLoop(w *fsnotify.Watcher, root string) {
	defer w.Close()

	timer := time.NewTimer(reindexDebounce)
	timer.Stop()
	changedPaths := make(map[string]bool)

	for {
		select {
		case event, ok := <-w.Events:
			if !ok {
				return
			}
			if event.Has(fsnotify.Create) {
				if fi, err := os.Stat(event.Name); err == nil && fi.IsDir() {
					_ = w.Add(event.Name)
				}
			}
			if hasWatchExt(event.Name) {
				changedPaths[event.Name] = true
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(reindexDebounce)
			}
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			s.log.Errorf("koseven-lsp: watcher: %v", err)
		case <-timer.C:
			paths := make([]string, 0, len(changedPaths))
			for p := range changedPaths {
				paths = append(paths, p)
			}
			changedPaths = make(map[string]bool)
			s.reindexFiles(root, paths)
		}
	}
}

func (s *Server) reindexFiles(root string, paths []string) {
	s.mu.RLock()
	curr, cfg := s.viewIdx, s.cfg
	s.mu.RUnlock()

	concreteIdx, ok := curr.(*view.ViewIndex)
	if !ok || concreteIdx == nil {
		s.reindex(root, cfg)
		return
	}

	next := concreteIdx
	var failed bool
	for _, path := range paths {
		updated, err := view.ReindexFile(path, next)
		if err != nil {
			s.log.Errorf("koseven-lsp: reindex %s: %v", path, err)
			failed = true
			continue
		}
		next = updated
	}
	if !failed {
		s.mu.Lock()
		s.viewIdx = next
		s.mu.Unlock()
	}
	s.log.Infof("koseven-lsp: incremental reindex complete (%d files)", len(paths))
}


func hasWatchExt(name string) bool {
	for _, ext := range watchExts {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}

// detectRoot extracts the project root from InitializeParams.
// Priority: WorkspaceFolders[0] > RootURI > RootPath > cwd.
func detectRoot(p *protocol.InitializeParams) string {
	if len(p.WorkspaceFolders) > 0 {
		return URIToPath(p.WorkspaceFolders[0].URI)
	}
	if p.RootURI != nil && *p.RootURI != "" {
		return URIToPath(*p.RootURI)
	}
	if p.RootPath != nil && *p.RootPath != "" {
		return *p.RootPath
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}
