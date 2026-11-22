package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var packDirs = []string{"behavior_packs", "resource_packs"}
var packID = regexp.MustCompile("(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$")

type Pack struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Version   [3]int `json:"version"`
	Enabled   bool   `json:"enabled"`
	Managed   bool   `json:"managed"`
	WorldOnly bool   `json:"worldOnly"`
	folder    string
}

func manifest(dir string) (Pack, error) {
	var p Pack
	f, err := os.Open(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return p, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() > 1<<20 {
		return p, errors.New("Pack manifest is too large.")
	}
	b, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil {
		return p, err
	}
	var m struct {
		Header struct {
			UUID, Name string
			Version    json.RawMessage
		}
		Modules []struct{ Type string }
	}
	if json.Unmarshal(bytes.TrimPrefix(b, []byte{0xef, 0xbb, 0xbf}), &m) != nil || !packID.MatchString(m.Header.UUID) {
		return p, errors.New("A pack must have a standard manifest header with a UUID.")
	}
	var v []int
	if err := json.Unmarshal(m.Header.Version, &v); err != nil {
		var str string
		if json.Unmarshal(m.Header.Version, &str) == nil {
			for _, s := range strings.Split(str, ".") {
				n, e := strconv.Atoi(s)
				if e != nil {
					return p, errors.New("Unsupported pack version.")
				}
				v = append(v, n)
			}
		}
	}
	if len(v) != 3 || v[0] < 0 || v[1] < 0 || v[2] < 0 {
		return p, errors.New("Use a Bedrock pack with a three-part version.")
	}
	copy(p.Version[:], v)
	for _, m := range m.Modules {
		switch m.Type {
		case "data", "script":
			p.Kind = "behavior_packs"
		case "resources":
			p.Kind = "resource_packs"
		}
	}
	if p.Kind == "" {
		return p, errors.New("Select a Bedrock behavior or resource pack.")
	}
	p.ID = strings.ToLower(m.Header.UUID)
	p.Name = m.Header.Name
	if strings.TrimSpace(p.Name) == "" {
		p.Name = p.ID
	}
	p.folder = dir
	return p, nil
}
func refsFile(world, kind string) string { return filepath.Join(world, "world_"+kind+".json") }
func readRefs(world, kind string) ([]json.RawMessage, error) {
	b, err := os.ReadFile(refsFile(world, kind))
	if errors.Is(err, os.ErrNotExist) {
		return []json.RawMessage{}, nil
	}
	if err != nil {
		return nil, err
	}
	var refs []json.RawMessage
	if err = json.Unmarshal(bytes.TrimPrefix(b, []byte{0xef, 0xbb, 0xbf}), &refs); err != nil {
		return nil, errors.New("A world pack list is invalid. Correct its JSON in the server folder first.")
	}
	return refs, nil
}
func refID(r json.RawMessage) string {
	var v struct {
		ID string `json:"pack_id"`
	}
	_ = json.Unmarshal(r, &v)
	return strings.ToLower(v.ID)
}
func (h *Hosting) packs(world string) ([]Pack, error) {
	stock := map[string]bool{}
	if b, e := os.ReadFile(filepath.Join(h.server, ".mcbe2friends-stock")); e == nil {
		for _, s := range strings.Split(string(b), "\n") {
			stock[s] = true
		}
	}
	var out []Pack
	for _, kind := range packDirs {
		refs, e := readRefs(world, kind)
		if e != nil {
			return nil, e
		}
		active := map[string]bool{}
		for _, r := range refs {
			active[refID(r)] = true
		}
		seen := map[string]bool{}
		for _, base := range []string{world, h.server} {
			dirs, err := os.ReadDir(filepath.Join(base, kind))
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
			for _, d := range dirs {
				local := base == world
				if !d.IsDir() || (!local && stock[kind+"/"+d.Name()]) {
					continue
				}
				p, err := manifest(filepath.Join(base, kind, d.Name()))
				if err != nil || p.Kind != kind || seen[p.ID] {
					continue
				}
				p.WorldOnly = local
				p.Enabled = active[p.ID]
				p.Managed = !local && d.Name() == "m2f-"+p.ID
				seen[p.ID] = true
				out = append(out, p)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
func (h *Hosting) changePack(id string, enable, remove bool) error {
	if !packID.MatchString(id) {
		return errors.New("Choose a pack from the library.")
	}
	c, e := h.config()
	if e != nil {
		return e
	}
	world, e := h.worldPath(c.World)
	if e != nil {
		return e
	}
	packs, e := h.packs(world)
	if e != nil {
		return e
	}
	for _, p := range packs {
		if p.ID == strings.ToLower(id) {
			if remove {
				if !p.Managed {
					return errors.New("Use the server folder to remove a manually installed pack.")
				}
				dirs, e := os.ReadDir(filepath.Join(h.server, "worlds"))
				if e != nil && !errors.Is(e, os.ErrNotExist) {
					return e
				}
				for _, d := range dirs {
					if d.IsDir() {
						refs, e := readRefs(filepath.Join(h.server, "worlds", d.Name()), p.Kind)
						if e != nil {
							return e
						}
						for _, r := range refs {
							if refID(r) == p.ID {
								return fmt.Errorf("Disable this pack in world %q before removing it.", d.Name())
							}
						}
					}
				}
				return os.RemoveAll(p.folder)
			}
			refs, e := readRefs(world, p.Kind)
			if e != nil {
				return e
			}
			next := make([]json.RawMessage, 0, len(refs)+1)
			for _, r := range refs {
				if refID(r) != p.ID {
					next = append(next, r)
				} else if enable {
					return nil
				}
			}
			if enable {
				b, _ := json.Marshal(map[string]any{"pack_id": p.ID, "version": p.Version})
				next = append(next, b)
			}
			if err := os.MkdirAll(world, 0700); err != nil {
				return err
			}
			b, _ := json.MarshalIndent(next, "", "  ")
			return atomicWrite(refsFile(world, p.Kind), b)
		}
	}
	return errors.New("This pack is no longer in the server folder.")
}
func readPackArchives(ctx context.Context, archive, stage string) ([]Pack, error) {
	budget := int64(512 << 20)
	outer := filepath.Join(stage, "outer")
	if err := os.Mkdir(outer, 0700); err != nil {
		return nil, err
	}
	extract := func(path, dir string) error {
		z, e := zip.OpenReader(path)
		if e != nil {
			return errors.New("This file is not a readable Bedrock ZIP archive.")
		}
		defer z.Close()
		return unpack(ctx, &z.Reader, dir, &budget, 16000)
	}
	if err := extract(archive, outer); err != nil {
		return nil, err
	}
	var packs []Pack
	var nested []string
	find := func(root string, allowNested bool) error {
		return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if e := ctx.Err(); e != nil {
				return e
			}
			if d.IsDir() {
				if d.Name() == "__MACOSX" {
					return filepath.SkipDir
				}
				if _, e := os.Stat(filepath.Join(path, "manifest.json")); e == nil {
					p, e := manifest(path)
					if e != nil {
						return e
					}
					packs = append(packs, p)
					return filepath.SkipDir
				}
				return nil
			}
			if allowNested && strings.EqualFold(filepath.Ext(path), ".mcpack") {
				nested = append(nested, path)
			}
			return nil
		})
	}
	if err := find(outer, true); err != nil {
		return nil, err
	}
	if len(nested) > 32 {
		return nil, errors.New("Import up to 32 bundled packs at once.")
	}
	for i, path := range nested {
		dir := filepath.Join(stage, fmt.Sprintf("nested-%d", i))
		if e := os.Mkdir(dir, 0700); e != nil {
			return nil, e
		}
		if e := extract(path, dir); e != nil {
			return nil, e
		}
		if e := find(dir, false); e != nil {
			return nil, e
		}
	}
	if len(packs) == 0 || len(packs) > 32 {
		return nil, errors.New("Select a .mcpack, .mcaddon, or ZIP containing up to 32 Bedrock pack manifests.")
	}
	return packs, nil
}
func (h *Hosting) importPacks(ctx context.Context, archive string) error {
	if !h.installed() {
		return errors.New("Install the server before importing packs.")
	}
	stage, err := os.MkdirTemp(h.root, ".packs-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	packs, err := readPackArchives(ctx, archive, stage)
	if err != nil {
		return err
	}
	c, e := h.config()
	if e != nil {
		return e
	}
	world, e := h.worldPath(c.World)
	if e != nil {
		return e
	}
	existing, e := h.packs(world)
	if e != nil {
		return e
	}
	ids := map[string]bool{}
	for _, p := range existing {
		ids[p.ID] = true
	}
	for _, p := range packs {
		dest := filepath.Join(h.server, p.Kind, "m2f-"+p.ID)
		if ids[p.ID] {
			return fmt.Errorf("%q is already installed or duplicated. Remove its old copy before importing.", p.Name)
		}
		if _, e := os.Lstat(dest); !errors.Is(e, os.ErrNotExist) {
			return errors.New("The destination pack folder already exists.")
		}
		ids[p.ID] = true
	}
	var moved []string
	for _, p := range packs {
		dest := filepath.Join(h.server, p.Kind, "m2f-"+p.ID)
		if err := os.Rename(p.folder, dest); err != nil {
			for _, path := range moved {
				_ = os.RemoveAll(path)
			}
			return err
		}
		moved = append(moved, dest)
	}
	return nil
}
