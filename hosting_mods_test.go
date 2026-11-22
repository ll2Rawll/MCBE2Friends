package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func testModBytes(t *testing.T, id string) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, e := z.Create("fabric.mod.json")
	if e != nil {
		t.Fatal(e)
	}
	if e = json.NewEncoder(w).Encode(map[string]string{"id": id, "environment": "*"}); e != nil {
		t.Fatal(e)
	}
	if e = z.Close(); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func TestJavaCatalogAndLegacyProfiles(t *testing.T) {
	h := testHosting(t)
	h.hw = Hardware{JavaSupported: true, Cores: 4, RAMGiB: 8}
	versions := javaChoices()
	if len(versions) != 29 || versions[0].ID != "26.3" || versions[len(versions)-1].ID != "1.19" {
		t.Fatal(versions)
	}
	seen := map[string]bool{}
	for _, r := range catalog.Releases {
		if seen[r.ID] || r.Vanilla.Size <= 0 || len(r.Vanilla.SHA256) != 64 || !slices.Contains(r.Fabric, "net.fabricmc:fabric-loader:"+catalog.Loader) {
			t.Fatal(r.ID)
		}
		seen[r.ID] = true
		_ = r.runtime()
		for _, coord := range r.Fabric {
			lib := catalog.Libraries[coord]
			if lib.Size <= 0 || len(lib.SHA256) != 64 || !strings.HasPrefix(lib.URL, "https://maven.fabricmc.net/") {
				t.Fatal(coord)
			}
		}
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "server.properties"), []byte("mcbe2friends-engine=paper\nmotd=Old grove\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := h.javaConfig(dir)
	if err != nil || c.Version != "26.3" {
		t.Fatal(c, err)
	}
	c.Version = "1.20.3"
	if c.validate(h.hw) == nil {
		t.Fatal("invented Paper build")
	}
	c.Engine = "fabric"
	if err = c.validate(h.hw); err != nil {
		t.Fatal(err)
	}
	c.Version = "1.18.2"
	if c.validate(h.hw) == nil {
		t.Fatal("unknown release")
	}
}
func TestModrinthExactCompatibilityAndPins(t *testing.T) {
	pf := func(id, game string) modVersion {
		v := modVersion{ID: id, Project: id[:1], Games: []string{game}, Loaders: []string{"fabric"}, Status: "listed", Kind: "release"}
		f := modFile{URL: "https://cdn.modrinth.com/data/" + v.Project + "/versions/" + id + "/test.jar", Filename: v.Project + ".jar", Size: 5, Primary: true}
		f.Hashes.SHA512 = strings.Repeat("a", 128)
		v.Files = []modFile{f}
		return v
	}
	all := map[string]modVersion{}
	for _, id := range []string{"A1", "B1", "C1", "C2", "D1", "X1"} {
		all[id] = pf(id, "1.19")
	}
	a := all["A1"]
	a.Dependencies = []modDependency{{"C", "", "required"}, {"B", "", "required"}, {"X", "", "optional"}}
	all["A1"] = a
	b := all["B1"]
	b.Dependencies = []modDependency{{"C", "C2", "required"}}
	all["B1"] = b
	x := all["X1"]
	x.Environment = "client_only"
	all["X1"] = x
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		id := parts[1]
		switch {
		case parts[0] == "version":
			_ = json.NewEncoder(w).Encode(all[id])
		case len(parts) == 3:
			if r.URL.Query().Get("loaders") != `["fabric"]` || r.URL.Query().Get("game_versions") != `["1.19"]` {
				t.Error(r.URL)
			}
			_ = json.NewEncoder(w).Encode([]modVersion{all[id+"1"]})
		default:
			_ = json.NewEncoder(w).Encode(modProject{ID: id, Title: "Mod " + id, Type: "mod", Server: "optional"})
		}
	}))
	defer srv.Close()
	r := modResolver{api: modrinthAPI{srv.URL, srv.Client()}}
	next, files, err := r.resolve(t.Context(), "1.19", "A", "A1", modInventory{})
	if err != nil || len(files) != 3 || next["C"].Version != "C2" || next["X"].Project != "" {
		t.Fatal(next, err)
	}
	if _, e := changeInstalled(next, "C", "disable"); e == nil {
		t.Fatal("disabled a dependency")
	}
	if _, _, e := r.resolve(t.Context(), "1.20", "A", "A1", modInventory{}); e == nil {
		t.Fatal("installed wrong game version")
	}
	if _, _, e := r.resolve(t.Context(), "1.19", "X", "X1", modInventory{}); e == nil {
		t.Fatal("accepted a client-only file")
	}
	a.Files[0].URL = "https://example.invalid/file.jar"
	if _, e := a.binary(); e == nil {
		t.Fatal("non-CDN binary")
	}
}
func TestModChangesPreserveManualFilesAndRecover(t *testing.T) {
	dir := t.TempDir()
	folder := filepath.Join(dir, "mods")
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	bind := func(p string, raw []byte) {
		t.Helper()
		if err := os.WriteFile(p, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	data := testModBytes(t, "grove")
	sha := sha512.Sum512(data)
	a := installedMod{Project: "A", Version: "A1", Title: "Grove", FabricID: "grove", File: "Grove.jar", Digest: hex.EncodeToString(sha[:]), Enabled: true}
	old := modInventory{"A": a}
	bind(filepath.Join(folder, a.File), data)
	bind(filepath.Join(folder, "Mine.jar"), testModBytes(t, "custom"))
	if err := writeModJSON(filepath.Join(folder, modManifest), modState{"first", old}); err != nil {
		t.Fatal(err)
	}
	next, e := changeInstalled(old, "A", "disable")
	if e != nil {
		t.Fatal(e)
	}
	commit := func(f func() error) error { return f() }
	if e = stageModChange(t.Context(), dir, old, next, nil, commit); e != nil {
		t.Fatal(e)
	}
	state, e := getModState(dir)
	if e != nil || state.Items["A"].Enabled || !verified(filepath.Join(folder, "Grove.jar.disabled"), a.Digest) {
		t.Fatal(state, e)
	}
	bind(filepath.Join(folder, "Manual.jar"), data)
	if e = stageModChange(t.Context(), dir, next, old, nil, commit); e == nil {
		t.Fatal("overwrote manual duplicate")
	}
	if e = os.Remove(filepath.Join(folder, "Manual.jar")); e != nil {
		t.Fatal(e)
	}
	// Simulate exiting between publishing the new JAR and publishing the manifest.
	tx := filepath.Join(dir, modTransaction)
	for _, sub := range []string{"old", "new"} {
		if e = os.MkdirAll(filepath.Join(tx, sub), 0700); e != nil {
			t.Fatal(e)
		}
	}
	if e = os.Rename(filepath.Join(folder, "Grove.jar.disabled"), filepath.Join(tx, "old", "Grove.jar.disabled")); e != nil {
		t.Fatal(e)
	}
	bind(filepath.Join(folder, "Grove.jar"), data)
	if e = writeModJSON(filepath.Join(tx, "journal.json"), modJournal{"interrupted", []string{"Grove.jar.disabled"}, []string{"Grove.jar"}}); e != nil {
		t.Fatal(e)
	}
	if e = recoverMods(dir); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(folder, "Grove.jar")); !os.IsNotExist(e) {
		t.Fatal("interrupted mod visible")
	}
	if !verified(filepath.Join(folder, "Grove.jar.disabled"), a.Digest) {
		t.Fatal("previous file was lost")
	}
	unmanaged, e := unmanagedMods(folder, next)
	if e != nil || !slices.Equal(unmanaged, []string{"Mine.jar"}) {
		t.Fatal(unmanaged, e)
	}
}
func TestSplitPaperWorldBackup(t *testing.T) {
	h := testHosting(t)
	dir := t.TempDir()
	cfg := h.hw.javaDefaults()
	cfg.Engine = "paper"
	cfg.Version = "1.19"
	if e := cfg.write(dir); e != nil {
		t.Fatal(e)
	}
	root := filepath.Join(dir, "worlds")
	for _, n := range []string{"world/region", "world_nether/DIM-1", "world_the_end/DIM1"} {
		if e := os.MkdirAll(filepath.Join(root, n), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(filepath.Join(root, n, "saved"), []byte(n), 0600); e != nil {
			t.Fatal(e)
		}
	}
	if e := os.WriteFile(filepath.Join(root, "world", "level.dat"), []byte{0x1f, 0x8b, 0x01}, 0600); e != nil {
		t.Fatal(e)
	}
	if e := backupJavaWorld(t.Context(), dir, "world"); e != nil {
		t.Fatal(e)
	}
	l, e := listJavaFiles(dir)
	if e != nil || !slices.Equal(l.Worlds, []string{"world"}) || len(l.Backups) != 1 {
		t.Fatal(l, e)
	}
	if e = restoreJavaBackup(t.Context(), dir, l.Backups[0], "restored"); e != nil {
		t.Fatal(e)
	}
	for _, n := range []string{"world/region", "world_nether/DIM-1", "world_the_end/DIM1"} {
		data, e := os.ReadFile(filepath.Join(root, strings.Replace(n, "world", "restored", 1), "saved"))
		if e != nil || string(data) != n {
			t.Fatal(n, e)
		}
	}
}
