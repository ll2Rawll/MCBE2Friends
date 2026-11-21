package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPastedAddressesAndConfig(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		port     int
	}{
		{" Play.Example.net:19223 ", "play.example.net", 19223},
		{"[2001:db8::2]:20000", "2001:db8::2", 20000},
		{"[::1]", "::1", 19132},
		{"2001:db8::2", "2001:db8::2", 19132},
		{"192.168.1.15", "192.168.1.15", 19132},
	} {
		config := Settings{Host: tc.in, Port: 19132, Name: `My "world": #1`}
		s, err := config.normalized()
		if err != nil || s.Host != tc.want || s.Port != tc.port {
			t.Fatalf("%q → %+v, %v", tc.in, s, err)
		}
		yaml := string(s.yaml())
		if !strings.Contains(yaml, `world-name: "My \"world\": #1"`) || !strings.Contains(yaml, "config-version: 9") {
			t.Fatal(yaml)
		}
	}
	for _, host := range []string{"", "https://example.net", "play server", "host:0", "host:65536", "1.2.3.400", "0.0.0.0", "test\nremote-port: 12"} {
		if _, err := (Settings{Host: host, Port: 19132}).normalized(); err == nil {
			t.Errorf("accepted invalid host %q", host)
		}
	}
}

func TestVerifiedDownloadNeverPublishesPartialOrModifiedFile(t *testing.T) {
	body := []byte("verified artifact")
	sum := sha256.Sum256(body)
	a := asset{"sample", hex.EncodeToString(sum[:]), int64(len(body))}
	dir := t.TempDir()
	dest := filepath.Join(dir, "component")
	for _, payload := range []string{string(body), "partial", strings.Repeat("X", len(body)), strings.Repeat("Y", len(body)+1)} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(payload)) }))
		err := download(context.Background(), srv.URL, dest, a, func(int) {})
		srv.Close()
		if (payload == string(body)) != (err == nil) {
			t.Fatalf("unexpected download result %v", err)
		}
		if !verified(dest, a.digest) {
			t.Fatal("previous verified component was lost")
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 1 {
			t.Fatal("temporary download was left behind")
		}
	}
}

func TestSessionSignalsAndPrivateEvents(t *testing.T) {
	l, err := newLauncher(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := &attempt{cancel: cancel, done: make(chan struct{})}
	l.active = a
	l.state.Phase = "connecting"
	l.handleLine(a, "[Auth] To sign in, use a web browser to open the page https://www.microsoft.com/link and enter the code TEST-1234 to authenticate.")
	if s := l.snapshot(); s.Phase != "signin" || s.Code != "TEST-1234" {
		t.Fatal("missing sign-in prompt")
	}
	l.handleLine(a, "[Primary Session] Successfully authenticated as Pine Player (1234567890) with 1/1000 friends")
	if s := l.snapshot(); s.Phase != "connecting" || s.Gamertag != "Pine Player" || s.Code != "" {
		t.Fatal("authentication must not imply a live session")
	}
	l.handleLine(a, "[Primary Session] Creation of Xbox LIVE session was successful!")
	if l.snapshot().Phase != "live" {
		t.Fatal("missing confirmed session")
	}
	l.handleLine(a, "[Primary Session] Connection to websocket lost, re-creating session...")
	if l.snapshot().Phase != "reconnecting" {
		t.Fatal("lost connection still marked live")
	}
	l.handleLine(a, "[Primary Session] WebSocket session reconnected")
	if l.snapshot().Phase != "live" {
		t.Fatal("reconnected session not recognized")
	}
	l.handleLine(a, "Got update session response: secret-response-body")
	l.handleLine(a, "[Auth] To sign in, use a web browser to open the page https://example.org/link and enter the code TEST-5678 to authenticate.")
	if ctx.Err() == nil || a.failure == "" {
		t.Fatal("unexpected sign-in destination accepted")
	}
	b, _ := json.Marshal(l.snapshot().Events)
	for _, v := range []string{"TEST-", "1234567890", "secret-response-body"} {
		if strings.Contains(string(b), v) {
			t.Fatal("private output appeared in activity")
		}
	}
}

func TestLocalAccessAndSavedSettings(t *testing.T) {
	root := t.TempDir()
	l, err := newLauncher(root)
	if err != nil {
		t.Fatal(err)
	}
	i := Instance{Port: 45123, Token: "private-test-key"}
	api := handler(l, i, func() {})
	post := func(host, origin, key string) int {
		req := httptest.NewRequest("POST", "http://"+host+"/api/settings", strings.NewReader(`{"host":"play.example.net","port":19132,"name":"Pine","autoFriend":true}`))
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		api.ServeHTTP(w, req)
		return w.Code
	}
	if post("127.0.0.1:45123", i.origin(), "wrong") == 200 || post("127.0.0.1:45123", "https://elsewhere.example", i.Token) == 200 || post("other.example:45123", "", i.Token) == 200 {
		t.Fatal("foreign request changed settings")
	}
	if post("127.0.0.1:45123", i.origin(), i.Token) != 200 {
		t.Fatal("local settings save failed")
	}
	restored, err := newLauncher(root)
	if err != nil || restored.snapshot().Settings != l.snapshot().Settings {
		t.Fatal("settings were not restored")
	}
	lock, err := lockInstance(filepath.Join(root, "instance.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if another, err := lockInstance(filepath.Join(root, "instance.lock")); err != errInUse {
		if another != nil {
			another.Close()
		}
		t.Fatal("parallel launch not locked")
	}
}

func TestDisconnectRemovesOnlyAccount(t *testing.T) {
	root := t.TempDir()
	l, err := newLauncher(root)
	if err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(root, "broadcast", "cache")
	if err := os.MkdirAll(cache, 0700); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"cache.json", "player_history.db"} {
		if err := os.WriteFile(filepath.Join(cache, file), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	l.active = &attempt{}
	if l.forgetAccount() == nil {
		t.Fatal("disconnected during active session")
	}
	l.active = nil
	if err := l.forgetAccount(); err != nil {
		t.Fatal(err)
	}
	if l.accountSaved() {
		t.Fatal("saved sign-in still present")
	}
	if _, err := os.Stat(filepath.Join(cache, "player_history.db")); err != nil {
		t.Fatal("player history was removed")
	}
}
