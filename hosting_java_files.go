package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

type javaPlugin struct {
	File    string `json:"file"`
	Enabled bool   `json:"enabled"`
	Managed bool   `json:"managed"`
}
type javaFiles struct {
	Worlds  []string     `json:"worlds"`
	Backups []string     `json:"backups"`
	Players []string     `json:"players"`
	Plugins []javaPlugin `json:"plugins"`
}

const viaFile = "Mcbe2Friends-ViaVersion.jar"

// A manually installed ViaVersion remains under the owner's control.
func checkViaPlugin(folder string) error {
	entries, err := os.ReadDir(folder)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || e.Name() == viaFile || !strings.HasSuffix(strings.ToLower(e.Name()), ".jar") {
			continue
		}
		z, err := zip.OpenReader(filepath.Join(folder, e.Name()))
		if err != nil {
			continue
		}
		desc, err := z.Open("plugin.yml")
		var d struct{ Name string }
		if err == nil {
			_ = yaml.NewDecoder(io.LimitReader(desc, 128<<10)).Decode(&d)
			desc.Close()
		}
		z.Close()
		if strings.EqualFold(d.Name, "ViaVersion") {
			return errors.New("Disable or move your installed ViaVersion plugin before turning on managed cross-play.")
		}
	}
	return nil
}

func managedPlugin(file string) bool {
	n := strings.ToLower(strings.TrimSuffix(file, ".disabled"))
	return n == "geyser-spigot.jar" || n == "floodgate-spigot.jar" || n == strings.ToLower(viaFile)
}
func javaFileName(s string) bool {
	return filepath.IsLocal(s) && filepath.Base(s) == s && !strings.ContainsAny(s, "/\\:") && !strings.HasPrefix(s, ".")
}
func listJavaFiles(dir string) (javaFiles, error) {
	var files javaFiles
	for _, folder := range []string{"worlds", "backups", "plugins"} {
		entries, err := os.ReadDir(filepath.Join(dir, folder))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return files, err
		}
		for _, e := range entries {
			n := e.Name()
			switch folder {
			case "worlds":
				if e.IsDir() && worldNameOK(n) && !paperSibling(dir, n) {
					files.Worlds = append(files.Worlds, n)
				}
			case "backups":
				if e.Type().IsRegular() && javaFileName(n) && strings.HasSuffix(n, ".zip") {
					files.Backups = append(files.Backups, n)
				}
			case "plugins":
				if e.Type().IsRegular() && javaFileName(n) && strings.HasSuffix(strings.ToLower(strings.TrimSuffix(n, ".disabled")), ".jar") {
					files.Plugins = append(files.Plugins, javaPlugin{n, !strings.HasSuffix(n, ".disabled"), managedPlugin(n)})
				}
			}
		}
	}
	b, err := os.ReadFile(filepath.Join(dir, "whitelist.json"))
	if err == nil {
		var players []struct{ Name string }
		if err = json.Unmarshal(b, &players); err != nil {
			return files, err
		}
		for _, p := range players {
			files.Players = append(files.Players, p.Name)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return files, err
	}
	return files, nil
}

var pluginName = regexp.MustCompile("^[A-Za-z0-9_.-]{1,64}$")

func importJavaPlugin(dir, source string, c JavaConfig) error {
	if c.Engine != "paper" {
		return errors.New("Choose a Paper server to use plugins.")
	}
	z, err := zip.OpenReader(source)
	if err != nil {
		return errors.New("Choose a readable Paper or Bukkit plugin JAR.")
	}
	defer z.Close()
	var name string
	for _, f := range z.File {
		if f.Name != "plugin.yml" && f.Name != "paper-plugin.yml" {
			continue
		}
		if f.UncompressedSize64 > 128<<10 {
			return errors.New("This plugin descriptor is too large.")
		}
		r, err := f.Open()
		if err != nil {
			return err
		}
		var info struct{ Name, Main string }
		err = yaml.NewDecoder(io.LimitReader(r, 128<<10)).Decode(&info)
		r.Close()
		if err != nil || info.Main == "" || !pluginName.MatchString(info.Name) {
			return errors.New("This JAR needs a valid Paper or Bukkit plugin descriptor.")
		}
		if strings.EqualFold(info.Name, "Geyser-Spigot") || strings.EqualFold(info.Name, "floodgate") {
			return errors.New("Manage Geyser and Floodgate together in Cross-play settings.")
		}
		if name != "" && !strings.EqualFold(info.Name, name) {
			return errors.New("The plugin descriptors have different names.")
		}
		name = info.Name
	}
	if name == "" {
		return errors.New("This JAR is missing plugin.yml or paper-plugin.yml.")
	}
	folder := filepath.Join(dir, "plugins")
	if err = os.MkdirAll(folder, 0700); err != nil {
		return err
	}
	dst := filepath.Join(folder, name+".jar")
	for _, p := range []string{dst, dst + ".disabled"} {
		if _, e := os.Lstat(p); !errors.Is(e, os.ErrNotExist) {
			return errors.New("This plugin already exists. Remove the old JAR before importing its replacement.")
		}
	}
	// Windows requires the ZIP reader to release the JAR before the move.
	if err = z.Close(); err != nil {
		return err
	}
	return os.Rename(source, dst)
}
func changeJavaPlugin(dir, file, action string, c JavaConfig) error {
	if c.Engine != "paper" || !javaFileName(file) || !strings.HasSuffix(strings.ToLower(strings.TrimSuffix(file, ".disabled")), ".jar") || managedPlugin(file) {
		return errors.New("Choose a player-installed Paper plugin.")
	}
	path := filepath.Join(dir, "plugins", file)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("This plugin file is unavailable.")
	}
	if action == "remove" {
		return os.Remove(path)
	}
	dst := ""
	if action == "enable" && strings.HasSuffix(path, ".disabled") {
		dst = strings.TrimSuffix(path, ".disabled")
	}
	if action == "disable" && !strings.HasSuffix(path, ".disabled") {
		dst = path + ".disabled"
	}
	if dst == "" {
		return errors.New("Choose an available plugin action.")
	}
	if _, e := os.Lstat(dst); !errors.Is(e, os.ErrNotExist) {
		return errors.New("A plugin with that filename already exists.")
	}
	return os.Rename(path, dst)
}
func backupJavaWorld(ctx context.Context, dir, name string) error {
	path, err := worldDirectory(dir, name)
	if err != nil {
		return err
	}
	if !javaWorldAt(path) {
		return errors.New("Start this Java world once before backing it up.")
	}
	roots := []backupRoot{{path, ""}}
	for _, d := range paperDimensions {
		sibling := filepath.Join(filepath.Dir(path), name+d.suffix)
		if isPaperDimension(sibling, d.folder) {
			roots = append(roots, backupRoot{sibling, paperArchive + "/" + d.folder + "/"})
		}
	}
	if len(roots) > 1 {
		roots[0].prefix = paperArchive + "/overworld/"
	}
	return backupDirectories(ctx, roots, filepath.Join(dir, "backups"), name, ".zip")
}

// Older Paper releases keep dimension metadata and regions in sibling worlds.
const paperArchive = "mcbe2friends-paper-dimensions-v1"

var paperDimensions = []struct{ suffix, folder string }{{"_nether", "DIM-1"}, {"_the_end", "DIM1"}}

func isPaperDimension(dir, sub string) bool {
	root, e := os.Lstat(dir)
	if e != nil || !root.IsDir() {
		return false
	}
	child, e := os.Lstat(filepath.Join(dir, sub))
	return e == nil && child.IsDir()
}
func paperSibling(dir, name string) bool {
	for _, d := range paperDimensions {
		main, ok := strings.CutSuffix(name, d.suffix)
		if ok && javaWorldAt(filepath.Join(dir, "worlds", main)) && isPaperDimension(filepath.Join(dir, "worlds", name), d.folder) {
			return true
		}
	}
	return false
}
func restorePaperDimensions(dir, src, dest string) error {
	p, err := readJavaProperties(dir)
	if err != nil {
		return err
	}
	if p.GetString("mcbe2friends-engine", "") != "paper" || !strings.HasPrefix(p.GetString("mcbe2friends-version", legacyJavaVersion), "1.") {
		return errors.New("Restore this split-dimension backup to a matching legacy Paper profile.")
	}
	main := filepath.Join(src, "overworld")
	if !javaWorldAt(main) {
		return errors.New("This Paper backup is missing the overworld.")
	}
	type move struct{ from, to string }
	var moves []move
	for _, d := range paperDimensions {
		from := filepath.Join(src, d.folder)
		ok, e := optionalFile(from)
		if e != nil {
			return e
		}
		if exists, e := optionalFile(dest + d.suffix); e != nil {
			return e
		} else if exists {
			return errors.New("Choose a new world name. A dimension with that name already exists.")
		}
		if ok {
			if !isPaperDimension(from, d.folder) {
				return errors.New("This saved Paper dimension is incomplete.")
			}
			moves = append(moves, move{from, dest + d.suffix})
		}
	}
	if len(moves) == 0 {
		return errors.New("This backup is missing its Paper dimensions.")
	}
	moves = append(moves, move{main, dest})
	for i, m := range moves {
		if err := os.Rename(m.from, m.to); err != nil {
			for j := i - 1; j >= 0; j-- {
				err = errors.Join(err, os.Rename(moves[j].to, moves[j].from))
			}
			return err
		}
	}
	return nil
}
func javaWorldAt(dir string) bool {
	f, err := os.Open(filepath.Join(dir, "level.dat"))
	if err != nil {
		return false
	}
	defer f.Close()
	var magic [2]byte
	_, err = io.ReadFull(f, magic[:])
	return err == nil && magic == [2]byte{0x1f, 0x8b}
}
func importJavaWorld(ctx context.Context, dir, source, name string) error {
	dest, err := worldDirectory(dir, name)
	if err != nil {
		return err
	}
	if _, e := os.Lstat(dest); !errors.Is(e, os.ErrNotExist) {
		return errors.New("Choose a new world folder name. That name already exists.")
	}
	stage, err := os.MkdirTemp(dir, ".world-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	z, err := zip.OpenReader(source)
	if err != nil {
		return errors.New("Choose a Java world ZIP archive.")
	}
	defer z.Close()
	limit := int64(8 << 30)
	if err = unpack(ctx, &z.Reader, stage, &limit, 150000); err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		return err
	}
	split := filepath.Join(stage, paperArchive)
	if _, e := os.Stat(filepath.Join(split, "overworld", "level.dat")); e == nil {
		return restorePaperDimensions(dir, split, dest)
	}
	src := stage
	if !javaWorldAt(src) {
		src = ""
		entries, err := os.ReadDir(stage)
		if err != nil {
			return err
		}
		for _, e := range entries {
			p := filepath.Join(stage, e.Name())
			if e.IsDir() && e.Name() != "__MACOSX" && javaWorldAt(p) {
				if src != "" {
					return errors.New("Choose a ZIP containing a single Java world.")
				}
				src = p
			}
		}
		if src == "" {
			return errors.New("This ZIP needs a Java world with a compressed level.dat at its root or in one enclosing folder.")
		}
	}
	return os.Rename(src, dest)
}
func restoreJavaBackup(ctx context.Context, dir, backup, name string) error {
	if !javaFileName(backup) || !strings.HasSuffix(backup, ".zip") {
		return errors.New("Choose a Java world backup.")
	}
	path := filepath.Join(dir, "backups", backup)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("The selected backup is unavailable.")
	}
	return importJavaWorld(ctx, dir, path, name)
}
