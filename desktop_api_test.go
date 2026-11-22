package main

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDesktopConnectionBoundary(t *testing.T) {
	var calls atomic.Int32
	worker := httptest.NewUnstartedServer(nil)
	instance := Instance{Port: worker.Listener.Addr().(*net.TCPAddr).Port, Token: "private-worker-token"}
	worker.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+instance.Token || r.Header.Get("Origin") != instance.origin() {
			t.Error("worker headers changed")
		}
		if r.URL.RequestURI() != "/api/state?selected=1" {
			t.Error("query or path changed")
		}
		w.Write([]byte("connected"))
	})
	worker.Start()
	defer worker.Close()
	connection, closeRelay, err := startWindowConnection(instance)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRelay()
	if connection.Token == instance.Token || connection.Origin == instance.origin() {
		t.Fatal("Worker identity was exposed")
	}
	for _, tc := range []struct {
		method, path, origin, auth, host string
		want                             int
	}{
		{"GET", "/api/state?selected=1", "http://wails.localhost", connection.Token, "", 200},
		{"GET", "/api/state?selected=1", "wails://wails", connection.Token, "", 200},
		{"OPTIONS", "/api/state", "null", "", "", 204},
		{"GET", "/api/state", "null", "wrong", "", 403},
		{"GET", "/api/state", "https://example.com", connection.Token, "", 403},
		{"GET", "/api/state", "http://wails.localhost", connection.Token, "wrong.local", 404},
		{"GET", "/desktop-connection.json", "http://wails.localhost", connection.Token, "", 404},
	} {
		req, _ := http.NewRequest(tc.method, connection.Origin+tc.path, nil)
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("Authorization", "Bearer "+tc.auth)
		if tc.host != "" {
			req.Host = tc.host
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != tc.want || strings.Contains(string(body), instance.Token) {
			t.Errorf("%s %s %s: status %d", tc.method, tc.origin, tc.path, resp.StatusCode)
		}
		if tc.want == 200 && resp.Header.Get("Access-Control-Allow-Origin") != tc.origin {
			t.Error("incorrect CORS response")
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("unexpected requests reached the worker: %d", calls.Load())
	}
}
