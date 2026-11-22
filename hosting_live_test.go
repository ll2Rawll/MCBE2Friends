package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Explicit opt-in: downloads Mojang's official server to a temporary directory.
func TestOfficialBedrockLifecycle(t *testing.T) {
	if os.Getenv("MCBE2FRIENDS_TEST_BEDROCK") != "1" {
		t.Skip("set MCBE2FRIENDS_TEST_BEDROCK=1 to exercise the official server download")
	}
	if runtime.GOARCH != "amd64" || runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skip("no native Bedrock server")
	}
	root := t.TempDir()
	load := func() *Launcher {
		worker, err := newLauncher(root)
		if err != nil {
			t.Fatal(err)
		}
		fixtureRegistry(t, worker.host)
		return worker
	}
	l := load()
	h := l.host
	if err := h.activate(context.Background(), approvalTestKey); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	c := h.hw.defaults()
	c.World = "Mcbe2Friends Test"
	c.Port = port
	if err = h.saveConfig(c); err != nil {
		t.Fatal(err)
	}
	if err := h.start(true); err != nil {
		t.Fatal(err)
	}
	for launch := 0; launch < 2; launch++ {
		if launch == 1 {
			l = load()
			h = l.host
			l.recoverServices(t.Context())
		}
		end := time.Now().Add(3 * time.Minute)
		for h.status().Phase != "running" {
			s := h.status()
			if s.Phase == "error" || s.Phase == "unreachable" || time.Now().After(end) {
				t.Fatalf("server: %s %s\n%s", s.Phase, s.Message, strings.Join(s.Log, "\n"))
			}
			time.Sleep(200 * time.Millisecond)
		}
		client := http.Client{Timeout: 3 * time.Second}
		res, e := client.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/v1/join")
		if e != nil {
			t.Fatal(e)
		}
		var info struct {
			Level    string
			Protocol int
		}
		e = json.NewDecoder(res.Body).Decode(&info)
		res.Body.Close()
		if e != nil || info.Level != c.World || info.Protocol == 0 {
			t.Fatalf("unexpected Bedrock signaling response: %+v %v", info, e)
		}
		if err := h.command("list"); err != nil {
			t.Fatal(err)
		}
		if l.closeControls() || !h.isBusy() {
			t.Fatal("closing controls stopped the running world")
		}
		// OS shutdown keeps intent. A player stopping the recovered server clears it.
		done, err := h.stop(launch == 0)
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Fatal("Bedrock did not stop")
		}
		if _, e := os.Stat(filepath.Join(h.server, "worlds", c.World, "level.dat")); e != nil {
			t.Fatal(e)
		}
	}
	if err := serve(root, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "launcher.json")); !os.IsNotExist(err) {
		t.Fatal("server restarted after explicit stop")
	}
}
