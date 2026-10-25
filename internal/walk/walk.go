package walk

import (
	"os"
	"path/filepath"
	"sort"
)

// Entry is one item of a directory listing.
type Entry struct {
	Name  string
	IsDir bool
}

// List returns the entries of dir that are not ignored: directories first, each group sorted by name.
// Symbolic links are listed with the type of their target.
func (ig *Ignorer) List(dir string) ([]Entry, error) {
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var dirs, files []Entry
	for _, de := range des {
		isDir := de.IsDir()
		if de.Type()&os.ModeSymlink != 0 {
			if st, err := os.Stat(filepath.Join(dir, de.Name())); err == nil {
				isDir = st.IsDir()
			}
		}
		if ig.Ignored(filepath.Join(dir, de.Name()), isDir) {
			continue
		}
		if isDir {
			dirs = append(dirs, Entry{Name: de.Name(), IsDir: true})
		} else {
			files = append(files, Entry{Name: de.Name()})
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name < dirs[j].Name })
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return append(dirs, files...), nil
}

// Dirs calls fn for start and every directory below it that is not ignored, in sorted pre-order.
// depth is 0 for start; maxDepth < 0 means no limit. Symbolic links to directories are not followed.
func (ig *Ignorer) Dirs(start string, maxDepth int, fn func(dir string, depth int) error) error {
	var visit func(dir string, depth int) error
	visit = func(dir string, depth int) error {
		if err := fn(dir, depth); err != nil {
			return err
		}
		if maxDepth >= 0 && depth >= maxDepth {
			return nil
		}
		des, err := os.ReadDir(dir)
		if err != nil {
			return nil
		}
		names := make([]string, 0, len(des))
		for _, de := range des {
			if de.IsDir() && de.Type()&os.ModeSymlink == 0 {
				names = append(names, de.Name())
			}
		}
		sort.Strings(names)
		for _, name := range names {
			child := filepath.Join(dir, name)
			if ig.Ignored(child, true) {
				continue
			}
			if err := visit(child, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(filepath.Clean(start), 0)
}
