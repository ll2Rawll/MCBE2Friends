package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestJavaPremiumAndIndependentIntent(t *testing.T) {
	l, err := newLauncher(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := l.host
	h.hw = Hardware{JavaSupported: true, Cores: 4, RAMGiB: 8}
	registry := fixtureRegistry(t, h)
	for _, endpoint := range []string{"/java", "/java/" + strings.Repeat("1", 24) + "/settings", "/java/" + strings.Repeat("1", 24) + "/command", "/java/" + strings.Repeat("1", 24) + "/mods/change"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "http://localhost/api/hosting"+endpoint, strings.NewReader("{}"))
		r.Header.Set("Content-Type", "application/json")
		hostingHTTP(h, w, r)
		if w.Code != 403 {
			t.Fatal("unlicensed action", endpoint, w.Code)
		}
	}
	if _, err = h.createJava("Locked", "paper", true, ""); err == nil {
		t.Fatal("unlicensed profile created")
	}
	if err = h.activate(t.Context(), approvalTestKey); err != nil {
		t.Fatal(err)
	}
	id1, err := h.createJava("First", "paper", true, "")
	if err != nil {
		t.Fatal(err)
	}
	id2, err := h.createJava("Second", "vanilla", false, "")
	if err != nil {
		t.Fatal(err)
	}
	d1, _ := h.javaServer(id1)
	c1, _ := h.javaConfig(d1)
	d2, _ := h.javaServer(id2)
	c2, _ := h.javaConfig(d2)
	if javaPortsOverlap(c1, c2) {
		t.Fatal("default port collision")
	}
	h.javaInstall <- struct{}{} // Hold both installers at their cancellable download gate.
	defer func() { <-h.javaInstall }()
	if err = h.startJava(id1, true, nil); err != nil {
		t.Fatal(err)
	}
	if err = h.startJava(id2, true, nil); err == nil {
		t.Fatal("exceeded combined heap budget")
	}
	c2.Memory = 1024
	c2.Port = c1.BedrockPort
	if err = c2.write(d2); err != nil {
		t.Fatal(err)
	}
	if err = h.startJava(id2, true, nil); err == nil {
		t.Fatal("reserved cross-play TCP port accepted")
	}
	c2.Port = c1.Port + 1
	if err = c2.write(d2); err != nil {
		t.Fatal(err)
	}
	if err = h.startJava(id2, true, nil); err != nil {
		t.Fatal(err)
	}
	if l.closeControls() || len(l.snapshot().JavaBusy) != 2 {
		t.Fatal("active profiles were detached")
	}
	if err = h.javaEdit(id1, func(string, JavaConfig) error { return nil }); err == nil {
		t.Fatal("changed a running server")
	}
	h.mu.Lock()
	done1 := h.java[id1].active.done
	done2 := h.java[id2].active.done
	h.mu.Unlock()
	if err = h.stopJava(id1); err != nil {
		t.Fatal(err)
	}
	<-done1
	if !slices.Equal(l.background.status().Java, []string{id2}) {
		t.Fatal("stop cleared another profile")
	}
	registry.Store(http.StatusNotFound)
	if err = h.activate(t.Context(), ""); err == nil {
		t.Fatal("withdrawn entitlement accepted")
	}
	select {
	case <-done2:
	case <-time.After(time.Second):
		t.Fatal("revocation left a Java installer active")
	}
	if err = h.startJava(id2, false, nil); err == nil {
		t.Fatal("withdrawn server restarted")
	}
	if err = h.stopJava(id2); err != nil || l.background.status().any() {
		t.Fatal("locked customer could not cancel restart", err)
	}
}

func TestJavaStandardFilesAndArchives(t *testing.T) {
	h := testHosting(t)
	h.hw = Hardware{JavaSupported: true, Cores: 4, RAMGiB: 8}
	dir := t.TempDir()
	c := h.hw.javaDefaults()
	c.Engine = "paper"
	c.Name = "Grove \u2603 " + string('\\') + " adventure"
	advanced := "# Keep this setting\nspawn-protection=7\ncustom.value=literal-$" + "{HOME}\n"
	if err := os.WriteFile(filepath.Join(dir, "server.properties"), []byte(advanced), 0600); err != nil {
		t.Fatal(err)
	}
	if err := c.write(dir); err != nil {
		t.Fatal(err)
	}
	loaded, err := h.javaConfig(dir)
	if err != nil || loaded != c {
		t.Fatal(loaded, err)
	}
	props, _ := readJavaProperties(dir)
	if props.GetString("custom.value", "") != "literal-$"+"{HOME}" || props.GetInt("spawn-protection", 0) != 7 {
		t.Fatal("custom properties changed")
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "server.properties"))
	if !bytes.Contains(raw, []byte("Keep this setting")) {
		t.Fatal("lost Java property comment")
	}
	c.Geyser = true
	if err = c.write(dir); err != nil {
		t.Fatal(err)
	}
	props, _ = readJavaProperties(dir)
	if props.GetBool("enforce-secure-profile", true) || !props.GetBool("online-mode", false) {
		t.Fatal("cross-play account or chat policy was incorrect")
	}
	c.Geyser = false
	if err = c.write(dir); err != nil {
		t.Fatal(err)
	}
	props, _ = readJavaProperties(dir)
	if !props.GetBool("enforce-secure-profile", false) {
		t.Fatal("Java-only chat policy was not restored")
	}
	path := filepath.Join(dir, "plugins", "Geyser-Spigot", "config.yml")
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("# original\nconfig-version: 12\nother-feature: true\nbedrock:\n  port: 10000\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = configureGeyser(dir, 19143); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(path)
	for _, s := range []string{"19143", "other-feature: true", "# original", "floodgate"} {
		if !bytes.Contains(raw, []byte(s)) {
			t.Fatal("Geyser settings", string(raw))
		}
	}
	archive := filepath.Join(dir, "upload.zip")
	writeZip := func(files map[string][]byte) {
		t.Helper()
		if e := os.WriteFile(archive, testZip(t, files), 0600); e != nil {
			t.Fatal(e)
		}
	}
	writeZip(map[string][]byte{"plugin.yml": []byte("name: GroveWelcome\nmain: org.example.Main\n")})
	if err = importJavaPlugin(dir, archive, c); err != nil {
		t.Fatal(err)
	}
	if err = changeJavaPlugin(dir, "GroveWelcome.jar", "disable", c); err != nil {
		t.Fatal(err)
	}
	if err = changeJavaPlugin(dir, "GroveWelcome.jar.disabled", "enable", c); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../GroveWelcome.jar", "Geyser-Spigot.jar", "floodgate-spigot.jar"} {
		if changeJavaPlugin(dir, name, "remove", c) == nil {
			t.Fatal("invalid plugin action", name)
		}
	}
	writeZip(map[string][]byte{"paper-plugin.yml": []byte("name: Floodgate\nmain: org.example.Main\n")})
	if importJavaPlugin(dir, archive, c) == nil {
		t.Fatal("managed plugin imported")
	}
	writeZip(map[string][]byte{"level.dat": {1, 2}, "db/file": {0}})
	if importJavaWorld(t.Context(), dir, archive, "World") == nil {
		t.Fatal("Bedrock archive imported as Java")
	}
	writeZip(map[string][]byte{"inner/level.dat": {0x1f, 0x8b, 0}, "inner/dimensions/minecraft/the_nether/region/file": {3}})
	if err = importJavaWorld(t.Context(), dir, archive, "My World"); err != nil {
		t.Fatal(err)
	}
	if importJavaWorld(t.Context(), dir, archive, "My World") == nil {
		t.Fatal("overwrote a world")
	}
	if err = backupJavaWorld(t.Context(), dir, "My World"); err != nil {
		t.Fatal(err)
	}
	files, err := listJavaFiles(dir)
	if err != nil || len(files.Backups) != 1 {
		t.Fatal(files, err)
	}
	if err = restoreJavaBackup(t.Context(), dir, files.Backups[0], "Restored"); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(dir, "worlds", "Restored", "dimensions", "minecraft", "the_nether", "region", "file")); err != nil {
		t.Fatal("dimension data missing", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err = restoreJavaBackup(ctx, dir, files.Backups[0], "Interrupted"); err == nil {
		t.Fatal("cancelled import committed")
	}
}
