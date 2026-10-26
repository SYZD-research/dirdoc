// Package resolve finds the project root, loads stored documents, and resolves display paths,
// references and inheritance from ancestor trees.
package resolve

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/SYZD-research/dirdoc/internal/config"
	"github.com/SYZD-research/dirdoc/internal/container"
	"github.com/SYZD-research/dirdoc/internal/model"
	"github.com/SYZD-research/dirdoc/internal/walk"
)

// Project is the context of one command: the root, the settings, and a cache of loaded documents.
type Project struct {
	Root   string
	Cwd    string
	Config *config.Config
	Ignore *walk.Ignorer

	dsds   map[string]dsdResult
	pfds   map[string]pfdResult
	hashes map[string]string // file → model.Hash of its content, "" when it cannot be read
}

type dsdResult struct {
	doc *model.DSD
	err error
}

type pfdResult struct {
	doc *model.PFD
	err error
}

// DocError reports a stored document that cannot be read.
type DocError struct {
	Path string
	Err  error
}

func (e *DocError) Error() string { return e.Path + ": " + e.Err.Error() }
func (e *DocError) Unwrap() error { return e.Err }

// Open finds the project root for target (an absolute path) and loads its dirdoc.toml.
func Open(cwd, target string) (*Project, error) {
	root := FindRoot(cwd, target)
	cfg, err := config.Load(filepath.Join(root, config.FileName))
	if err != nil {
		return nil, err
	}
	return &Project{
		Root:   root,
		Cwd:    cwd,
		Config: cfg,
		Ignore: walk.NewIgnorer(root, cfg.Walk.Gitignore, cfg.Walk.Ignore),
		dsds:   map[string]dsdResult{},
		pfds:   map[string]pfdResult{},
		hashes: map[string]string{},
	}, nil
}

// FindRoot returns the nearest ancestor of target holding dirdoc.toml, else the nearest holding .git,
// else the current directory when target is inside it, else target's directory.
func FindRoot(cwd, target string) string {
	start := filepath.Clean(target)
	if st, err := os.Stat(start); err != nil || !st.IsDir() {
		start = filepath.Dir(start)
	}
	for _, marker := range []string{config.FileName, ".git"} {
		for dir := start; ; dir = filepath.Dir(dir) {
			if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
				return dir
			}
			if filepath.Dir(dir) == dir {
				break
			}
		}
	}
	if within(cwd, start) {
		return filepath.Clean(cwd)
	}
	return start
}

// DSDFile returns the path of the DSD describing dir.
func DSDFile(dir string) string { return filepath.Join(dir, model.DirName, model.DSDName) }

// PFDFile returns the path of the PFD describing file.
func PFDFile(file string) string {
	return filepath.Join(filepath.Dir(file), model.DirName, filepath.Base(file)+model.PFDSuffix)
}

// InsideDirdoc reports whether abs is a .dirdoc directory or lies inside one.
func InsideDirdoc(abs string) bool {
	for _, part := range strings.Split(filepath.ToSlash(abs), "/") {
		if part == model.DirName {
			return true
		}
	}
	return false
}

// ErrSource reports a .dirdoc_* file that holds a TOML source instead of a stored document, as the
// merge driver leaves one it could not merge.
var ErrSource = errors.New("the file holds a TOML source, not a stored document: resolve the merge " +
	"conflict in it, then store it with `dirdoc compile TARGET FILE`")

// LoadFile decodes any .dirdoc_* file.
func LoadFile(path string) (model.Kind, *model.DSD, *model.PFD, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, nil, nil, &DocError{Path: path, Err: err}
	}
	js, _, err := container.Decode(data)
	if errors.Is(err, container.ErrUnknownFormat) && bytes.HasPrefix(data, []byte("# dirdoc ")) {
		err = ErrSource
	}
	if err != nil {
		return 0, nil, nil, &DocError{Path: path, Err: err}
	}
	kind, d, p, err := model.Decode(js)
	if err != nil {
		return 0, nil, nil, &DocError{Path: path, Err: err}
	}
	return kind, d, p, nil
}

func loadKind(path string, want model.Kind) (*model.DSD, *model.PFD, error) {
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	kind, d, p, err := LoadFile(path)
	if err != nil {
		return nil, nil, err
	}
	if kind != want {
		return nil, nil, &DocError{Path: path, Err: fmt.Errorf("expected a %s, found a %s", want, kind)}
	}
	return d, p, nil
}

// DSD returns the stored DSD of dir, or nil when there is none.
func (p *Project) DSD(dir string) (*model.DSD, error) {
	dir = filepath.Clean(dir)
	if r, ok := p.dsds[dir]; ok {
		return r.doc, r.err
	}
	d, _, err := loadKind(DSDFile(dir), model.KindDSD)
	p.dsds[dir] = dsdResult{doc: d, err: err}
	return d, err
}

// PFD returns the stored PFD of file, or nil when there is none.
func (p *Project) PFD(file string) (*model.PFD, error) {
	file = filepath.Clean(file)
	if r, ok := p.pfds[file]; ok {
		return r.doc, r.err
	}
	_, d, err := loadKind(PFDFile(file), model.KindPFD)
	p.pfds[file] = pfdResult{doc: d, err: err}
	return d, err
}

// SetDSD makes dir resolve to d, as compile does with the document it is about to write.
func (p *Project) SetDSD(dir string, d *model.DSD) { p.dsds[filepath.Clean(dir)] = dsdResult{doc: d} }

// SetPFD makes file resolve to d.
func (p *Project) SetPFD(file string, d *model.PFD) { p.pfds[filepath.Clean(file)] = pfdResult{doc: d} }

// Stale reports whether file changed since pf was compiled (design §10.5). It returns nil when that
// cannot be told: pf has no target_hash (it was written before Phase 2) or the file cannot be read.
func (p *Project) Stale(file string, pf *model.PFD) *bool {
	if pf == nil || pf.TargetHash == "" {
		return nil
	}
	file = filepath.Clean(file)
	h, ok := p.hashes[file]
	if !ok {
		if data, err := os.ReadFile(file); err == nil {
			h = model.Hash(data)
		}
		p.hashes[file] = h
	}
	if h == "" {
		return nil
	}
	stale := h != pf.TargetHash
	return &stale
}

// Document is a stored document found by Documents. DSD or PFD is set unless Err is.
type Document struct {
	Owner Owner
	DSD   *model.DSD
	PFD   *model.PFD
	Err   error
}

// Texts calls fn with every text of the document that may hold references.
func (d Document) Texts(fn func(id string, text *string)) {
	switch {
	case d.DSD != nil:
		d.DSD.Texts(fn)
	case d.PFD != nil:
		d.PFD.Texts(fn)
	}
}

// Documents calls fn for every document below the directory start, as check visits them: each
// directory's DSD, then its PFDs, directories in sorted pre-order.
func (p *Project) Documents(start string, fn func(Document)) {
	p.Ignore.Dirs(start, -1, func(dir string, _ int) error {
		l, err := List(dir)
		if err != nil || !l.Exists {
			return nil
		}
		if l.HasDSD {
			d, err := p.DSD(dir)
			fn(Document{Owner: DirOwner(dir), DSD: d, Err: err})
		}
		for _, name := range l.PFDs {
			file := filepath.Join(dir, name)
			pf, err := p.PFD(file)
			fn(Document{Owner: FileOwner(file), PFD: pf, Err: err})
		}
		return nil
	})
}

// Listing is the content of a .dirdoc directory.
type Listing struct {
	Exists  bool
	HasDSD  bool
	PFDs    []string // names of the documented files, sorted
	Unknown []string // anything else
}

// List reads dir/.dirdoc.
func List(dir string) (Listing, error) {
	des, err := os.ReadDir(filepath.Join(dir, model.DirName))
	if errors.Is(err, fs.ErrNotExist) {
		return Listing{}, nil
	}
	if err != nil {
		return Listing{}, err
	}
	l := Listing{Exists: true}
	for _, de := range des {
		name := de.Name()
		switch {
		case de.IsDir():
			l.Unknown = append(l.Unknown, name+"/")
		case name == model.DSDName:
			l.HasDSD = true
		case strings.HasSuffix(name, model.PFDSuffix) && len(name) > len(model.PFDSuffix):
			l.PFDs = append(l.PFDs, strings.TrimSuffix(name, model.PFDSuffix))
		default:
			l.Unknown = append(l.Unknown, name)
		}
	}
	sort.Strings(l.PFDs)
	sort.Strings(l.Unknown)
	return l, nil
}

// HasDirdoc reports whether dir has its own .dirdoc directory.
func HasDirdoc(dir string) bool {
	st, err := os.Stat(filepath.Join(dir, model.DirName))
	return err == nil && st.IsDir()
}

// Rel returns abs relative to the current directory, with a trailing slash for directories ("./" for the cwd).
func (p *Project) Rel(abs string, isDir bool) string {
	rel, err := filepath.Rel(p.Cwd, abs)
	if err != nil {
		rel = abs
	}
	rel = filepath.ToSlash(rel)
	if isDir {
		if rel == "." {
			return "./"
		}
		return strings.TrimSuffix(rel, "/") + "/"
	}
	return rel
}

// Within reports whether abs is the project root or inside it.
func (p *Project) Within(abs string) bool { return within(p.Root, abs) }

func within(root, abs string) bool {
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	return rel != ".." && !strings.HasPrefix(rel, "../")
}

func relSlash(base, abs string) string {
	rel, err := filepath.Rel(base, abs)
	if err != nil || rel == "." {
		return ""
	}
	return filepath.ToSlash(rel)
}

// Owner is the target a document describes: a directory (DSD) or a file (PFD).
type Owner struct {
	Kind model.Kind
	Path string
}

// DirOwner returns the owner of a DSD.
func DirOwner(dir string) Owner { return Owner{Kind: model.KindDSD, Path: filepath.Clean(dir)} }

// FileOwner returns the owner of a PFD.
func FileOwner(file string) Owner { return Owner{Kind: model.KindPFD, Path: filepath.Clean(file)} }

// Dir returns the directory relative paths in the document are resolved from.
func (o Owner) Dir() string {
	if o.Kind == model.KindDSD {
		return o.Path
	}
	return filepath.Dir(o.Path)
}

// Ref returns the display ref of the owner itself ("src/" or "src/main.rs").
func (p *Project) Ref(o Owner) string { return p.Rel(o.Path, o.Kind == model.KindDSD) }

// EntryRef returns the display ref of an entry ("src/main.rs#run").
func (p *Project) EntryRef(o Owner, id string) string { return p.Ref(o) + "#" + id }

// Inherited is the description of a directory found in an ancestor's tree.
type Inherited struct {
	Dir  string // the ancestor whose DSD describes the directory
	DSD  *model.DSD
	Node *model.TreeNode
	Rel  string // path of the node relative to Dir
}

// Entry returns the DSD entry the node links to, or nil.
func (i *Inherited) Entry() *model.DSDEntry {
	if i.Node.DSD == "" {
		return nil
	}
	return i.DSD.Entry(i.Node.DSD)
}

// Inherited looks for dir in the trees of its ancestors, nearest first, and returns the first node
// that links to an entry or carries a note (design §10.3).
func (p *Project) Inherited(dir string) *Inherited { return p.Described(dir, true) }

// Described is Inherited for a file or a directory: the nearest ancestor tree node for abs that
// links to an entry or carries a note, or nil.
func (p *Project) Described(abs string, isDir bool) *Inherited {
	abs = filepath.Clean(abs)
	for anc := filepath.Dir(abs); p.Within(anc); anc = filepath.Dir(anc) {
		if d, err := p.DSD(anc); err == nil && d != nil && d.Tree != nil {
			rel := relSlash(anc, abs)
			if n := d.Tree.Find(rel); n != nil && n.IsDir() == isDir && (n.DSD != "" || n.Note != "") {
				return &Inherited{Dir: anc, DSD: d, Node: n, Rel: rel}
			}
		}
		if anc == p.Root || filepath.Dir(anc) == anc {
			break
		}
	}
	return nil
}
