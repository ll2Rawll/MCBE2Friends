package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
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
	h := newHosting(t.TempDir())
	key := "M2F-CCCCCCCCCCCCCCCCCCCCCCCCCC"
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+grantDigest(h.device, key)+".txt" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintln(w, grantMarker)
	}))
	defer registry.Close()
	h.grants = registry.URL + "/"
	if e := h.activate(context.Background(), key); e != nil {
		t.Fatal(e)
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
	defer func() { <-h.stop(true) }()
	for launch := 0; launch < 2; launch++ {
		if e := h.start(launch == 0); e != nil {
			t.Fatal(e)
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
		select {
		case <-h.stop(false):
		case <-time.After(30 * time.Second):
			t.Fatal("Bedrock did not stop")
		}
		if _, e := os.Stat(filepath.Join(h.server, "worlds", c.World, "level.dat")); e != nil {
			t.Fatal(e)
		}
	}
}
