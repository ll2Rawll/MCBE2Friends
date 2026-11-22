package main

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostingPresetsAndStandardProperties(t *testing.T) {
	for _, tc := range []struct {
		cores               int
		ram                 float64
		tier                string
		players, view, tick int
	}{{2, 4, "Small", 4, 8, 4}, {8, 6, "Small", 4, 8, 4}, {4, 8, "Balanced", 8, 12, 4}, {12, 16, "Large", 12, 16, 6}, {2, 32, "Small", 4, 8, 4}} {
		tier, players, view, tick, threads := recommended(tc.cores, tc.ram)
		if tier != tc.tier || players != tc.players || view != tc.view || tick != tc.tick || threads >= tc.cores {
			t.Fatalf("unexpected preset for %+v", tc)
		}
	}
	path := filepath.Join(t.TempDir(), "server.properties")
	start := "# Hand-edited settings\r\nlevel-name=Old\r\nserver-port=19132\r\nserver-udp-ports=19132\r\ncustom-property=preserve me\r\n"
	os.WriteFile(path, []byte(start), 0600)
	c := (Hardware{Cores: 4, RAMGiB: 8}).defaults()
	c.World = "Meadow"
	c.Port = 19500
	if err := c.apply(path); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	out := string(b)
	for _, expect := range []string{"# Hand-edited settings\r\n", "custom-property=preserve me\r\n", "level-name=Meadow\r\n", "server-udp-ports=19500\r\n"} {
		if !strings.Contains(out, expect) {
			t.Fatalf("lost property %q", expect)
		}
	}
	out = strings.ReplaceAll(out, "server-udp-ports=19500", "server-udp-ports=29500:19500")
	os.WriteFile(path, []byte(out), 0600)
	c.Port = 19501
	if err := c.apply(path); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if !strings.Contains(string(b), "29500:19500") {
		t.Fatal("overwrote a custom UDP mapping")
	}
	for _, world := range []string{"../outside", "a/b", "C:world", "world.", "NUL", "COM1.sav", "a\nworld"} {
		if worldNameOK(world) {
			t.Errorf("accepted %q", world)
		}
	}
}

func testZip(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for n, data := range files {
		f, e := z.Create(n)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = f.Write(data); e != nil {
			t.Fatal(e)
		}
	}
	if e := z.Close(); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func TestHostingArchiveBoundsAndPackScope(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		budget int64
	}{{"../unrelated", 100}, {"C:/world", 100}, {"file", 1}} {
		data := testZip(t, map[string][]byte{tc.name: []byte("abcdef")})
		z, _ := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if unpack(ctx, z, t.TempDir(), &tc.budget, 5) == nil {
			t.Fatal("unbounded archive accepted")
		}
	}
	h := newHosting(t.TempDir())
	if e := os.MkdirAll(h.server, 0700); e != nil {
		t.Fatal(e)
	}
	for _, kind := range packDirs {
		os.Mkdir(filepath.Join(h.server, kind), 0700)
	}
	os.WriteFile(filepath.Join(h.server, bedrockExe()), []byte("fixture"), 0700)
	os.WriteFile(filepath.Join(h.server, "server.properties"), []byte("level-name=First\n"), 0600)
	id := "11111111-2222-4333-8444-555555555555"
	manifest := []byte(`{"header":{"uuid":"` + id + `","name":"Trees","version":[1,2,3]},"modules":[{"type":"data"}]}`)
	inner := testZip(t, map[string][]byte{"manifest.json": manifest})
	bundle := testZip(t, map[string][]byte{"pack.mcpack": inner})
	input := filepath.Join(h.root, "fixture.mcaddon")
	os.WriteFile(input, bundle, 0600)
	if err := h.importPacks(ctx, input); err != nil {
		t.Fatal(err)
	}
	if err := h.importPacks(ctx, input); err == nil {
		t.Fatal("overwrote an installed pack")
	}
	if err := h.changePack(id, true, false); err != nil {
		t.Fatal(err)
	}
	c, _ := h.config()
	c.World = "Second"
	if e := h.saveConfig(c); e != nil {
		t.Fatal(e)
	}
	if err := h.changePack(id, false, true); err == nil {
		t.Fatal("removed a pack still active in the first world")
	}
	c.World = "First"
	h.saveConfig(c)
	if err := h.changePack(id, false, false); err != nil {
		t.Fatal(err)
	}
	if err := h.changePack(id, false, true); err != nil {
		t.Fatal(err)
	}
	if _, e := os.Stat(filepath.Join(h.server, "behavior_packs", "m2f-"+id)); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("pack still present")
	}
}
