package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Downloads official game, loader and mod artifacts; explicitly opt in to accepting the EULA.
func TestOfficialFabric(t *testing.T) {
	if os.Getenv("MCBE2FRIENDS_TEST_FABRIC") != "1" {
		t.Skip("enable the Fabric server integration test explicitly")
	}
	l, err := newLauncher(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := l.host
	fixtureRegistry(t, h)
	if err = h.activate(t.Context(), approvalTestKey); err != nil {
		t.Fatal(err)
	}
	games := []string{"1.19", "1.21.1", javaVersion}
	if os.Getenv("MCBE2FRIENDS_TEST_FULL_CATALOG") == "1" {
		games = nil
		for _, r := range catalog.Releases {
			games = append(games, r.ID)
		}
	}
	for _, game := range games {
		t.Run(game, func(t *testing.T) {
			id, err := h.createJava("Fabric "+game, "fabric", false, game)
			if err != nil {
				t.Fatal(err)
			}
			dir, _ := h.javaServer(id)
			c, err := h.javaConfig(dir)
			if err != nil {
				t.Fatal(err)
			}
			ln, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			c.Port = ln.Addr().(*net.TCPAddr).Port
			ln.Close()
			c.Memory, c.View, c.Simulation = 1408, 4, 3
			if err = c.write(dir); err != nil {
				t.Fatal(err)
			}
			project, version, ingame := "", "", ""
			switch game {
			case "1.19":
				project, version, ingame = "LVN9ygNV", "KwzHK3mh", "ledger"
			case javaVersion:
				project, version, ingame = "gvQqBUqZ", "Lal8mJGR", "lithium"
			}
			if project != "" {
				found, e := modrinth.search(t.Context(), game, ingame, 0)
				if e != nil || !slices.ContainsFunc(found.Hits, func(x modHit) bool { return x.ID == project }) {
					t.Fatal("Modrinth search", found, e)
				}
				if e = h.updateMods(t.Context(), id, project, version, "install"); e != nil {
					t.Fatal(e)
				}
				if game == "1.19" {
					list, e := h.modList(id, dir)
					if e != nil || len(list.Managed) != 3 {
						t.Fatal(list, e)
					}
					if e = h.updateMods(t.Context(), id, "P7dR8mSH", "", "remove"); e == nil {
						t.Fatal("removed a required dependency")
					}
					for _, act := range []string{"disable", "enable"} {
						if e = h.updateMods(t.Context(), id, project, "", act); e != nil {
							t.Fatal(e)
						}
					}
				}
			}
			if err = h.startJava(id, true, nil); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = h.stopJava(id) })
			wait := func(phase string) {
				t.Helper()
				for end := time.Now().Add(5 * time.Minute); ; time.Sleep(180 * time.Millisecond) {
					h.mu.Lock()
					st := h.javaStateLocked(id).snapshot(true)
					h.mu.Unlock()
					if st.Phase == phase {
						return
					}
					if st.Phase == "error" || st.Phase == "unreachable" || time.Now().After(end) {
						t.Fatalf("%s: %s\n%s", st.Phase, st.Message, strings.Join(st.Log, "\n"))
					}
				}
			}
			wait("running")
			if e := javaPing(t.Context(), "127.0.0.1", c.Port); e != nil {
				t.Fatal(e)
			}
			if e := h.javaCommand(id, "list"); e != nil {
				t.Fatal(e)
			}
			if e := h.stopJava(id); e != nil {
				t.Fatal(e)
			}
			wait("offline")
			log, e := os.ReadFile(filepath.Join(dir, "logs", "latest.log"))
			if e != nil || !strings.Contains(string(log), fmt.Sprintf("Loading Minecraft %s with Fabric Loader %s", game, catalog.Loader)) {
				t.Fatalf("wrong loader: %v %s", e, log)
			}
			if ingame != "" && !strings.Contains(string(log), "- "+ingame+" ") {
				t.Fatal("mod did not load", ingame)
			}
			if e := backupJavaWorld(t.Context(), dir, c.World); e != nil {
				t.Fatal(e)
			}
			r, _ := findJavaRelease(game)
			t.Log("Minecraft " + game + ", Fabric Loader " + catalog.Loader + ", Java " + strconv.Itoa(r.Java) + " answered a status ping and saved its world")
		})
	}
}
