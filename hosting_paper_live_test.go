package main

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOfficialArchivedPaper(t *testing.T) {
	if os.Getenv("MCBE2FRIENDS_TEST_ARCHIVED_PAPER") != "1" {
		t.Skip("enable archived Paper downloads and the Minecraft EULA explicitly")
	}
	l, e := newLauncher(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	h := l.host
	fixtureRegistry(t, h)
	if e = h.activate(t.Context(), approvalTestKey); e != nil {
		t.Fatal(e)
	}
	for _, g := range []string{"1.19", "1.21.1"} {
		t.Run(g, func(t *testing.T) {
			cross := g == "1.21.1"
			id, e := h.createJava("Archived Paper "+g, "paper", cross, g)
			if e != nil {
				t.Fatal(e)
			}
			dir, _ := h.javaServer(id)
			c, e := h.javaConfig(dir)
			if e != nil {
				t.Fatal(e)
			}
			port := func() int {
				ln, e := net.Listen("tcp4", "127.0.0.1:0")
				if e != nil {
					t.Fatal(e)
				}
				defer ln.Close()
				return ln.Addr().(*net.TCPAddr).Port
			}
			c.Port = port()
			c.BedrockPort = port()
			for c.Port == c.BedrockPort {
				c.BedrockPort = port()
			}
			c.Memory, c.View, c.Simulation = 1536, 4, 3
			if e = c.write(dir); e != nil {
				t.Fatal(e)
			}
			if e = h.startJava(id, true, nil); e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() { _ = h.stopJava(id) })
			wait := func(phase string) {
				t.Helper()
				for until := time.Now().Add(5 * time.Minute); ; time.Sleep(180 * time.Millisecond) {
					h.mu.Lock()
					s := h.javaStateLocked(id).snapshot(true)
					h.mu.Unlock()
					if s.Phase == phase {
						return
					}
					if s.Phase == "error" || s.Phase == "unreachable" || time.Now().After(until) {
						t.Fatalf("%s: %s\n%s", s.Phase, s.Message, strings.Join(s.Log, "\n"))
					}
				}
			}
			wait("running")
			if e = javaPing(t.Context(), "127.0.0.1", c.Port); e != nil {
				t.Fatal(e)
			}
			if cross {
				if e = geyserPing(t.Context(), c.BedrockPort); e != nil {
					t.Fatal(e)
				}
			}
			if e = h.stopJava(id); e != nil {
				t.Fatal(e)
			}
			wait("offline")
			if cross {
				log, e := os.ReadFile(filepath.Join(dir, "logs", "latest.log"))
				if e != nil || !strings.Contains(string(log), "ViaVersion detected server version: 1.21") {
					t.Fatal("ViaVersion did not register on the older Paper server", e)
				}
			}
			if e = backupJavaWorld(t.Context(), dir, c.World); e != nil {
				t.Fatal(e)
			}
			f, e := listJavaFiles(dir)
			if e != nil || len(f.Backups) != 1 {
				t.Fatal(f, e)
			}
			if e = restoreJavaBackup(t.Context(), dir, f.Backups[0], "Restored"); e != nil {
				t.Fatal(e)
			}
			for _, d := range paperDimensions {
				if !isPaperDimension(filepath.Join(dir, "worlds", "Restored"+d.suffix), d.folder) {
					t.Fatal("Missing restored dimension", d)
				}
			}
			t.Log("Paper saved and restored the overworld, Nether and End. Managed cross-play:", cross)
		})
	}
}
