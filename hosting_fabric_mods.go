package main

import (
	"archive/zip"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

type modRule struct {
	Project string `json:"project"`
	Version string `json:"version,omitempty"`
}
type installedMod struct {
	Project   string    `json:"project"`
	Title     string    `json:"title"`
	Version   string    `json:"version"`
	Number    string    `json:"number"`
	File      string    `json:"file"`
	Digest    string    `json:"sha512"`
	FabricID  string    `json:"fabricId"`
	Enabled   bool      `json:"enabled"`
	Requires  []modRule `json:"requires,omitempty"`
	Conflicts []modRule `json:"conflicts,omitempty"`
}
type modInventory map[string]installedMod

func (m installedMod) filename() string {
	if m.Enabled {
		return m.File
	}
	return m.File + ".disabled"
}
func (m modInventory) validate() error {
	ids := map[string]string{}
	filenames := map[string]bool{}
	for key, item := range m {
		digest, e := hex.DecodeString(item.Digest)
		if key != item.Project || !modID.MatchString(key) || !modID.MatchString(item.Version) || e != nil || len(digest) != 64 || !modJarName(item.File) {
			return errors.New("The saved mod inventory contains an invalid entry.")
		}
		file := strings.ToLower(item.File)
		if filenames[file] {
			return fmt.Errorf("Two projects use the filename %s.", item.File)
		}
		filenames[file] = true
		if !item.Enabled {
			continue
		}
		if other := ids[item.FabricID]; item.FabricID != "" && other != "" {
			return fmt.Errorf("%s and %s both provide the Fabric mod %s.", other, item.Title, item.FabricID)
		}
		ids[item.FabricID] = item.Title
		for _, d := range item.Requires {
			dep, ok := m[d.Project]
			if !ok || !dep.Enabled || d.Version != "" && d.Version != dep.Version {
				return fmt.Errorf("%s needs %s. Disable or change the dependent mod first.", item.Title, modName(m, d.Project))
			}
		}
		for _, d := range item.Conflicts {
			dep, ok := m[d.Project]
			if ok && dep.Enabled && (d.Version == "" || dep.Version == d.Version) {
				return fmt.Errorf("%s is incompatible with %s.", item.Title, dep.Title)
			}
		}
	}
	return nil
}
func modName(inv modInventory, id string) string {
	if item, ok := inv[id]; ok {
		return item.Title
	}
	return id
}
func (r *modResolver) resolve(ctx context.Context, game, project, version string, existing modInventory) (modInventory, map[string]modFile, error) {
	if !modID.MatchString(project) || !modID.MatchString(version) {
		return nil, nil, errors.New("Choose a project and one of its compatible versions.")
	}
	r.game = game
	r.versions = map[string]modVersion{}
	r.projects = map[string]modProject{}
	r.choices = map[string][]modVersion{}
	// Resolve broad requirements again when another branch pins an exact file.
	hints := map[string]string{project: version}
	for range 16 {
		pins := map[string]string{project: version}
		planned := modInventory{}
		files := map[string]modFile{}
		var visit func(string) error
		visit = func(id string) error {
			if _, seen := planned[id]; seen {
				return nil
			}
			if len(planned) >= 64 {
				return errors.New("This install exceeds the 64-project dependency limit.")
			}
			p, ok := r.projects[id]
			if !ok {
				var err error
				p, err = r.api.project(ctx, id)
				if err != nil {
					return err
				}
				r.projects[id] = p
			}
			chosen := hints[id]
			if chosen == "" {
				chosen = pins[id]
			}
			if chosen == "" {
				chosen = existing[id].Version
			}
			v, err := r.choose(ctx, id, chosen)
			if err != nil {
				return err
			}
			file, err := v.binary()
			if err != nil {
				return fmt.Errorf("%s: %w", p.Title, err)
			}
			item := installedMod{Project: id, Title: p.Title, Version: v.ID, Number: v.Number, File: file.Filename, Digest: file.Hashes.SHA512, Enabled: true}
			if old := existing[id]; old.Digest == item.Digest {
				item.FabricID = old.FabricID
			}
			for _, d := range v.Dependencies {
				if d.Type != "required" && d.Type != "incompatible" {
					continue
				}
				if !modID.MatchString(d.Project) {
					if !modID.MatchString(d.Version) {
						return fmt.Errorf("%s has a required or conflicting file without a Modrinth project.", p.Title)
					}
					var meta modVersion
					if err := r.api.get(ctx, "/version/"+d.Version, nil, &meta); err != nil {
						return err
					}
					if meta.ID != d.Version || !modID.MatchString(meta.Project) {
						return errors.New("Invalid dependency metadata from Modrinth.")
					}
					d.Project = meta.Project
				}
				if d.Version != "" && !modID.MatchString(d.Version) {
					return errors.New("Invalid Modrinth dependency version.")
				}
				rule := modRule{d.Project, d.Version}
				if d.Type == "incompatible" {
					item.Conflicts = append(item.Conflicts, rule)
					continue
				}
				item.Requires = append(item.Requires, rule)
				if d.Version != "" {
					if prev := pins[d.Project]; prev != "" && prev != d.Version {
						return fmt.Errorf("Two mods require different files for %s. Choose another version.", modName(existing, d.Project))
					}
					pins[d.Project] = d.Version
				}
			}
			planned[id] = item
			files[id] = file
			for _, d := range item.Requires {
				if err := visit(d.Project); err != nil {
					return err
				}
			}
			return nil
		}
		if err := visit(project); err != nil {
			return nil, nil, err
		}
		mismatch := false
		for id, want := range pins {
			if planned[id].Version != want {
				mismatch = true
			}
		}
		if mismatch {
			hints = pins
			continue
		}
		next := maps.Clone(existing)
		maps.Copy(next, planned)
		return next, files, next.validate()
	}
	return nil, nil, errors.New("These dependency versions could not be resolved together.")
}

func changeInstalled(inv modInventory, project, action string) (modInventory, error) {
	item, ok := inv[project]
	if !ok {
		return nil, errors.New("Choose an installed Modrinth project.")
	}
	next := maps.Clone(inv)
	switch action {
	case "remove":
		delete(next, project)
	case "disable":
		item.Enabled = false
		next[project] = item
	case "enable":
		seen := map[string]bool{}
		var enable func(string) error
		enable = func(id string) error {
			if seen[id] {
				return nil
			}
			seen[id] = true
			m, ok := next[id]
			if !ok {
				return fmt.Errorf("Reinstall %s from Modrinth to restore missing dependencies.", item.Title)
			}
			m.Enabled = true
			next[id] = m
			for _, d := range m.Requires {
				if err := enable(d.Project); err != nil {
					return err
				}
			}
			return nil
		}
		if err := enable(project); err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("Choose enable, disable, or remove.")
	}
	if action == "remove" {
		for _, m := range next {
			if slices.ContainsFunc(m.Requires, func(d modRule) bool { return d.Project == project }) {
				return nil, fmt.Errorf("%s needs %s. Remove the dependent mod first.", m.Title, item.Title)
			}
		}
	}
	return next, next.validate()
}

var fabricIDPattern = regexp.MustCompile("^[a-z][a-z0-9_-]{1,63}$")

func readFabricMod(file string) (string, error) {
	z, err := zip.OpenReader(file)
	if err != nil {
		return "", errors.New("The selected file is not a readable Fabric mod JAR.")
	}
	defer z.Close()
	for _, f := range z.File {
		if f.Name != "fabric.mod.json" {
			continue
		}
		if f.UncompressedSize64 > 1<<20 {
			return "", errors.New("The Fabric descriptor exceeds the size limit.")
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		defer rc.Close()
		var d struct{ ID, Environment string }
		if err = json.NewDecoder(io.LimitReader(rc, 1<<20)).Decode(&d); err != nil || !fabricIDPattern.MatchString(d.ID) {
			return "", errors.New("The JAR contains an invalid Fabric descriptor.")
		}
		if d.Environment == "client" {
			return "", errors.New("This Fabric file runs only on a player's client.")
		}
		return d.ID, nil
	}
	return "", errors.New("This JAR does not contain fabric.mod.json.")
}

func unmanagedMods(folder string, inv modInventory) ([]string, error) {
	entries, err := os.ReadDir(folder)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	owned := map[string]bool{}
	for _, m := range inv {
		owned[strings.ToLower(m.filename())] = true
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() && javaFileName(n) && strings.HasSuffix(strings.ToLower(strings.TrimSuffix(n, ".disabled")), ".jar") && !owned[strings.ToLower(n)] {
			out = append(out, n)
		}
	}
	return out, nil
}
func verifyLooseMods(folder string, old, next modInventory) error {
	unmanaged, err := unmanagedMods(folder, old)
	if err != nil {
		return err
	}
	ids := map[string]string{}
	names := map[string]bool{}
	for _, m := range next {
		if m.Enabled {
			ids[m.FabricID] = m.Title
		}
		names[strings.ToLower(m.File)] = true
	}
	for _, n := range unmanaged {
		if names[strings.ToLower(strings.TrimSuffix(n, ".disabled"))] {
			return fmt.Errorf("Move the manually installed %s out of mods before installing this project.", n)
		}
		if strings.HasSuffix(n, ".disabled") {
			continue
		}
		id, e := readFabricMod(filepath.Join(folder, n))
		if e == nil && ids[id] != "" {
			return fmt.Errorf("%s is already provided by the manually installed %s.", ids[id], n)
		}
	}
	return nil
}
