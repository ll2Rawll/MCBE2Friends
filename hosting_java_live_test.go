package main

import (
	"math/rand/v2"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Opt-in because the official downloads are large and the test accepts the Minecraft EULA.
func TestOfficialJavaLifecycle(t *testing.T) {
	if os.Getenv("MCBE2FRIENDS_TEST_JAVA") != "1" {
		t.Skip("enable the official Java integration test explicitly")
	}
	root := t.TempDir()
	load := func() *Launcher {
		l, err := newLauncher(root)
		if err != nil {
			t.Fatal(err)
		}
		fixtureRegistry(t, l.host)
		return l
	}
	l := load()
	h := l.host
	if !h.hw.JavaSupported {
		t.Fatal("Java hosting unavailable on this runner", h.hw)
	}
	if e := h.activate(t.Context(), approvalTestKey); e != nil {
		t.Fatal(e)
	}
	port := func() int {
		t.Helper()
		// An OS-assigned TCP port can already be a UDP client port on Windows.
		// Use both protocols on all interfaces, outside the normal ephemeral range.
		for range 100 {
			candidate := 10000 + rand.IntN(20000)
			addr := net.JoinHostPort("0.0.0.0", strconv.Itoa(candidate))
			tcp, e := net.Listen("tcp4", addr)
			if e != nil {
				continue
			}
			udp, e := net.ListenPacket("udp4", addr)
			tcp.Close()
			if e == nil {
				udp.Close()
				return candidate
			}
		}
		t.Fatal("could not find an available Minecraft test port")
		return 0
	}
	configure := func(id string) JavaConfig {
		var c JavaConfig
		e := h.javaEdit(id, func(dir string, cfg JavaConfig) error {
			c = cfg
			c.Port = port()
			c.BedrockPort = port()
			for c.BedrockPort == c.Port {
				c.BedrockPort = port()
			}
			c.Memory, c.View, c.Simulation = 1536, 4, 3
			return c.write(dir)
		})
		if e != nil {
			t.Fatal(e)
		}
		return c
	}
	wait := func(id, phase string) {
		t.Helper()
		for end := time.Now().Add(5 * time.Minute); ; {
			h.mu.Lock()
			s := h.javaStateLocked(id).snapshot(true)
			h.mu.Unlock()
			if s.Phase == phase {
				return
			}
			if s.Phase == "error" || s.Phase == "unreachable" || time.Now().After(end) {
				t.Fatalf("%s: %s %s\n%s", id, s.Phase, s.Message, strings.Join(s.Log, "\n"))
			}
			time.Sleep(150 * time.Millisecond)
		}
	}
	vx, err := h.createJava("Vanilla forest", "vanilla", false, "")
	if err != nil {
		t.Fatal(err)
	}
	v := configure(vx)
	if h.startJava(vx, false, nil) == nil {
		t.Fatal("started without EULA consent")
	}
	if err = h.startJava(vx, true, nil); err != nil {
		t.Fatal(err)
	}
	wait(vx, "running")
	if err = javaPing(t.Context(), "127.0.0.1", v.Port); err != nil {
		t.Fatal(err)
	}
	if err = h.javaCommand(vx, "list"); err != nil {
		t.Fatal(err)
	}
	vd, _ := h.javaServer(vx)
	if _, e := os.Stat(filepath.Join(vd, "plugins", "Geyser-Spigot.jar")); !os.IsNotExist(e) {
		t.Fatal("unexpected Geyser installation")
	}
	if l.closeControls() {
		t.Fatal("closing the panel stopped Java")
	}
	if err = h.javaCommand(vx, "minecraft:stop"); err != nil {
		t.Fatal(err)
	}
	wait(vx, "offline")
	if err = backupJavaWorld(t.Context(), vd, v.World); err != nil {
		t.Fatal(err)
	}
	f, err := listJavaFiles(vd)
	if err != nil || len(f.Backups) != 1 {
		t.Fatal(f, err)
	}
	if err = restoreJavaBackup(t.Context(), vd, f.Backups[0], "Restored forest"); err != nil {
		t.Fatal(err)
	}
	if !javaWorldAt(filepath.Join(vd, "worlds", "Restored forest")) {
		t.Fatal("restored world missing")
	}
	t.Log("Vanilla answered a Minecraft ping, stopped, and restored its saved world")
	px, err := h.createJava("Paper grove", "paper", false, "")
	if err != nil {
		t.Fatal(err)
	}
	p := configure(px)
	if err = h.startJava(px, true, nil); err != nil {
		t.Fatal(err)
	}
	wait(px, "running")
	if err = h.stopJava(px); err != nil {
		t.Fatal(err)
	}
	wait(px, "offline")
	pd, _ := h.javaServer(px)
	world := filepath.Join(pd, "worlds", p.World)
	if err = os.WriteFile(filepath.Join(world, "test-world-marker"), []byte("persisted"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = h.javaEdit(px, func(dir string, c JavaConfig) error { c.Geyser = true; return c.write(dir) }); err != nil {
		t.Fatal(err)
	}
	if err = h.startJava(px, false, nil); err != nil {
		t.Fatal(err)
	}
	wait(px, "running")
	if err = geyserPing(t.Context(), p.BedrockPort); err != nil {
		t.Fatal(err)
	}
	if err = javaPing(t.Context(), "127.0.0.1", p.Port); err != nil {
		t.Fatal(err)
	}
	if err = h.javaCommand(px, "geyser version"); err != nil {
		t.Fatal(err)
	}
	if l.closeControls() {
		t.Fatal("Geyser did not survive panel close")
	}
	done, err := h.stop(true)
	if err != nil {
		t.Fatal(err)
	}
	<-done
	l = load()
	h = l.host
	l.recoverServices(t.Context())
	wait(px, "running")
	if !slices.Contains(h.background.status().Java, px) || slices.Contains(h.background.status().Java, vx) {
		t.Fatal("wrong profiles restored")
	}
	if err = h.stopJava(px); err != nil {
		t.Fatal(err)
	}
	wait(px, "offline")
	if err = h.javaEdit(px, func(dir string, c JavaConfig) error { c.Geyser = false; return c.write(dir) }); err != nil {
		t.Fatal(err)
	}
	if err = h.startJava(px, false, nil); err != nil {
		t.Fatal(err)
	}
	wait(px, "running")
	for _, name := range []string{"Geyser-Spigot.jar", "floodgate-spigot.jar"} {
		if _, e := os.Stat(filepath.Join(pd, "plugins", name)); !os.IsNotExist(e) {
			t.Fatal("managed plugin stayed enabled", name, e)
		}
	}
	if _, e := os.Stat(filepath.Join(world, "test-world-marker")); e != nil {
		t.Fatal("cross-play toggle changed worlds", e)
	}
	if s, e := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(p.BedrockPort)), time.Second); e == nil {
		s.Close()
		t.Fatal("Bedrock listener stayed enabled")
	}
	if err = h.stopJava(px); err != nil {
		t.Fatal(err)
	}
	wait(px, "offline")
	if l.background.status().any() {
		t.Fatal("stopped servers still scheduled")
	}
	t.Log("Paper answered with and without Geyser, resumed after worker restart, and retained its world")
}
