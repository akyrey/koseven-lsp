package project

// RootKind identifies which layer of the Koseven cascading filesystem a path
// belongs to.
type RootKind int

const (
	RootApplication RootKind = iota // application/views/
	RootModule                      // modules/<name>/views/
	RootSystem                      // system/views/
)

// Module describes an enabled Koseven module and its filesystem path.
type Module struct {
	Name string // key from Kohana::modules([...]), e.g. "blog"
	Path string // absolute path to the module directory
}

// ViewRoot is a directory that participates in the Koseven view cascade.
type ViewRoot struct {
	Path       string
	Kind       RootKind
	Order      int
	ModuleName string // "" for application/system roots
}
