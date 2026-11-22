package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

type windowConnection struct {
	Origin string `json:"origin"`
	Token  string `json:"token"`
}

func nativeOrigin(origin string) bool {
	// WebKit can serialize its private application scheme as an opaque origin.
	return origin == "wails://wails" || origin == "http://wails.localhost" || origin == "null"
}

// Stream files through ordinary HTTP on every engine. Only this window gets
// the temporary relay credential. Existing workers keep their own credential
// and origin checks, including workers from the previous release.
func startWindowConnection(worker Instance) (windowConnection, func(), error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return windowConnection{}, nil, err
	}
	addr := listener.Addr().String()
	info := windowConnection{Origin: "http://" + addr, Token: rand.Text()}
	transport := &http.Transport{Proxy: nil}
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != addr || !strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		origin := r.Header.Get("Origin")
		if !nativeOrigin(origin) {
			http.Error(w, "Invalid window origin.", http.StatusForbidden)
			return
		}
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Cache-Control", "no-store")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Private-Network", "true")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+info.Token)) != 1 {
			http.Error(w, "Open the launcher controls.", http.StatusForbidden)
			return
		}
		endpoint := worker.origin() + r.URL.EscapedPath()
		if r.URL.RawQuery != "" {
			endpoint += "?" + r.URL.RawQuery
		}
		req, err := http.NewRequestWithContext(r.Context(), r.Method, endpoint, r.Body)
		if err != nil {
			http.Error(w, "Invalid action.", http.StatusBadRequest)
			return
		}
		req.ContentLength = r.ContentLength
		req.Header.Set("Authorization", "Bearer "+worker.Token)
		req.Header.Set("Origin", worker.origin())
		req.Header.Set("Content-Type", r.Header.Get("Content-Type"))
		resp, err := client.Do(req)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "The background service is unavailable. Reopen Mcbe2Friends to reconnect."})
			return
		}
		defer resp.Body.Close()
		for _, name := range []string{"Content-Type", "Content-Disposition", "Content-Length"} {
			if v := resp.Header.Get(name); v != "" {
				w.Header().Set(name, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	})
	go server.Serve(listener)
	closeRelay := func() { server.Close(); transport.CloseIdleConnections() }
	return info, closeRelay, nil
}

func closeDesktopControls(i Instance) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, i.origin()+"/api/quit", nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+i.Token)
	client := http.Client{Transport: &http.Transport{DisableKeepAlives: true}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if resp, err := client.Do(req); err == nil {
		resp.Body.Close()
	}
}
