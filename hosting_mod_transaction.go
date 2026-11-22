package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const modManifest = ".mcbe2friends-mods.json"
const modTransaction = ".mcbe2friends-mod-transaction"

type modState struct {
	Revision string       `json:"revision"`
	Items    modInventory `json:"items"`
}
type modJournal struct {
	Revision string
	Old, New []string
}

func getModState(dir string) (modState, error) {
	s := modState{Items: modInventory{}}
	data, err := os.ReadFile(filepath.Join(dir, "mods", modManifest))
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err = json.Unmarshal(data, &s); err != nil || s.Items == nil {
		return s, errors.New("The saved Modrinth inventory could not be read.")
	}
	return s, s.Items.validate()
}
func optionalFile(path string) (bool, error) {
	_, e := os.Lstat(path)
	if errors.Is(e, os.ErrNotExist) {
		return false, nil
	}
	return e == nil, e
}
func plainDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s must be a normal directory.", filepath.Base(path))
	}
	return nil
}
func writeModJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, data)
}

// The manifest revision is the commit marker. An interrupted operation restores
// only the JARs it moved, leaving the rest of the player's mods directory intact.
func recoverMods(dir string) error {
	tx := filepath.Join(dir, modTransaction)
	exists, err := optionalFile(tx)
	if err != nil || !exists {
		return err
	}
	if err = plainDirectory(tx); err != nil {
		return err
	}
	journal, err := os.ReadFile(filepath.Join(tx, "journal.json"))
	if errors.Is(err, os.ErrNotExist) {
		return os.RemoveAll(tx)
	}
	if err != nil {
		return err
	}
	var j modJournal
	if err = json.Unmarshal(journal, &j); err != nil || j.Revision == "" {
		return errors.New("The pending mod installation record is unreadable.")
	}
	for _, group := range [][]string{j.New, j.Old} {
		for _, name := range group {
			if !modJarName(strings.TrimSuffix(name, ".disabled")) {
				return errors.New("The pending mod record contains an invalid filename.")
			}
		}
	}
	state, err := getModState(dir)
	if err != nil {
		return err
	}
	if state.Revision != j.Revision {
		mods := filepath.Join(dir, "mods")
		if err = plainDirectory(mods); err != nil {
			return err
		}
		for _, name := range j.New {
			staged := filepath.Join(tx, "new", name)
			ok, e := optionalFile(staged)
			if e != nil {
				return e
			}
			if !ok {
				if e = os.Rename(filepath.Join(mods, name), staged); e != nil {
					return fmt.Errorf("Recovering %s: %w", name, e)
				}
			}
		}
		for _, name := range j.Old {
			saved := filepath.Join(tx, "old", name)
			ok, e := optionalFile(saved)
			if e != nil {
				return e
			}
			if ok {
				if e = os.Rename(saved, filepath.Join(mods, name)); e != nil {
					return e
				}
			}
		}
	}
	return os.RemoveAll(tx)
}
func copyMod(from, to string) error {
	if err := os.Link(from, to); err == nil {
		return nil
	}
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, src)
	closeErr := dst.Close()
	if err != nil {
		return err
	}
	return closeErr
}
func (h *Hosting) updateMods(ctx context.Context, id, project, version, action string) (err error) {
	var dir, game string
	err = h.javaEdit(id, func(d string, c JavaConfig) error {
		if c.Engine != "fabric" {
			return errors.New("Choose a Fabric server to manage mods.")
		}
		if err := recoverMods(d); err != nil {
			return err
		}
		h.modWork[id] = true
		dir, game = d, c.Version
		return nil
	})
	if err != nil {
		return err
	}
	defer func() { h.ops.Lock(); delete(h.modWork, id); h.ops.Unlock() }()
	state, err := getModState(dir)
	if err != nil {
		return err
	}
	var next modInventory
	var files map[string]modFile
	if action == "install" {
		resolver := modResolver{api: modrinth}
		next, files, err = resolver.resolve(ctx, game, project, version, state.Items)
	} else {
		next, err = changeInstalled(state.Items, project, action)
	}
	if err != nil {
		return err
	}
	return stageModChange(ctx, dir, state.Items, next, files, func(commit func() error) error {
		h.ops.Lock()
		defer h.ops.Unlock()
		if err := h.allowed(false); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return commit()
	})
}
func stageModChange(ctx context.Context, dir string, old, next modInventory, files map[string]modFile, commit func(func() error) error) (result error) {
	folder := filepath.Join(dir, "mods")
	if err := os.MkdirAll(folder, 0700); err != nil {
		return err
	}
	if err := plainDirectory(folder); err != nil {
		return err
	}
	tx := filepath.Join(dir, modTransaction)
	if err := os.Mkdir(tx, 0700); err != nil {
		return err
	}
	defer func() {
		if err := recoverMods(dir); err != nil {
			result = errors.Join(result, err)
		}
	}()
	for _, sub := range []string{"new", "old"} {
		if err := os.Mkdir(filepath.Join(tx, sub), 0700); err != nil {
			return err
		}
	}
	journal := modJournal{Revision: rand.Text()}
	oldNames := map[string]bool{}
	for _, id := range slices.Sorted(maps.Keys(old)) {
		m := old[id]
		n, ok := next[id]
		if ok && m.filename() == n.filename() && m.Digest == n.Digest {
			continue
		}
		src := filepath.Join(folder, m.filename())
		if exists, e := optionalFile(src); e != nil {
			return e
		} else if !exists && !ok {
			continue
		}
		if info, e := os.Lstat(src); e != nil || !info.Mode().IsRegular() || !verified(src, m.Digest) {
			return fmt.Errorf("%s was changed outside the launcher. Move the changed JAR out of mods before removing its entry.", m.filename())
		}
		journal.Old = append(journal.Old, m.filename())
		oldNames[strings.ToLower(m.filename())] = true
	}
	var bytes int64
	for _, id := range slices.Sorted(maps.Keys(next)) {
		m := next[id]
		o, ok := old[id]
		if ok && o.filename() == m.filename() && o.Digest == m.Digest {
			if _, selected := files[id]; !selected {
				continue
			}
			if verified(filepath.Join(folder, m.filename()), m.Digest) {
				continue
			}
			return fmt.Errorf("The installed %s has changed. Remove its entry and reinstall it.", m.File)
		}
		name := m.filename()
		dest := filepath.Join(tx, "new", name)
		if exists, e := optionalFile(filepath.Join(folder, name)); e != nil {
			return e
		} else if exists && !oldNames[strings.ToLower(name)] {
			return fmt.Errorf("A file named %s already exists in mods.", name)
		}
		if ok && o.Digest == m.Digest {
			if err := copyMod(filepath.Join(folder, o.filename()), dest); err != nil {
				return err
			}
		} else {
			f, ok := files[id]
			if !ok {
				return errors.New("Reinstall this project to restore its files.")
			}
			bytes += f.Size
			if bytes > 512<<20 {
				return errors.New("This install exceeds the 512 MiB dependency download limit.")
			}
			if err := download(ctx, f.URL, dest, asset{m.Title, m.Digest, f.Size}, func(int) {}); err != nil {
				return fmt.Errorf("%s: %w", m.Title, err)
			}
		}
		fabricID, err := readFabricMod(dest)
		if err != nil {
			return fmt.Errorf("%s: %w", m.Title, err)
		}
		m.FabricID = fabricID
		next[id] = m
		journal.New = append(journal.New, name)
	}
	if err := next.validate(); err != nil {
		return err
	}
	if err := verifyLooseMods(folder, old, next); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return commit(func() error {
		if err := writeModJSON(filepath.Join(tx, "journal.json"), journal); err != nil {
			return err
		}
		for _, name := range journal.Old {
			if err := os.Rename(filepath.Join(folder, name), filepath.Join(tx, "old", name)); err != nil {
				return err
			}
		}
		for _, name := range journal.New {
			if exists, e := optionalFile(filepath.Join(folder, name)); e != nil {
				return e
			} else if exists {
				return fmt.Errorf("A file named %s appeared in mods during installation.", name)
			}
			if err := os.Rename(filepath.Join(tx, "new", name), filepath.Join(folder, name)); err != nil {
				return err
			}
		}
		return writeModJSON(filepath.Join(folder, modManifest), modState{journal.Revision, next})
	})
}
