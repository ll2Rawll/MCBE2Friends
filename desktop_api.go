package main

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// The native asset handler is reachable only inside this window. The worker
// token stays in Go, including when the window attaches to an older live worker.
func desktopAPI(i Instance) http.Handler {
	target := i.origin()
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true}
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("Authorization") != "Bearer desktop-window" {
			http.Error(w, "Open the launcher controls.", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "null" {
			u, err := url.Parse(origin)
			if err != nil || !strings.EqualFold(u.Host, r.Host) {
				http.Error(w, "Invalid window origin.", http.StatusForbidden)
				return
			}
		}
		endpoint := target + r.URL.EscapedPath()
		if r.URL.RawQuery != "" {
			endpoint += "?" + r.URL.RawQuery
		}
		req, err := http.NewRequestWithContext(r.Context(), r.Method, endpoint, r.Body)
		if err != nil {
			http.Error(w, "Invalid action.", http.StatusBadRequest)
			return
		}
		req.ContentLength = r.ContentLength
		req.Header.Set("Authorization", "Bearer "+i.Token)
		req.Header.Set("Origin", target)
		req.Header.Set("Content-Type", r.Header.Get("Content-Type"))
		resp, err := client.Do(req)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "The background service is unavailable. Reopen Mcbe2Friends to reconnect."})
			return
		}
		defer resp.Body.Close()
		for _, name := range []string{"Content-Type", "Content-Disposition", "Content-Length", "Cache-Control"} {
			if v := resp.Header.Get(name); v != "" {
				w.Header().Set(name, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	})
}

func closeDesktopControls(i Instance) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, i.origin()+"/api/quit", nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+i.Token)
	// Closing the controls asks the worker to exit only when all services are idle.
	client := http.Client{Transport: &http.Transport{DisableKeepAlives: true}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if resp, err := client.Do(req); err == nil {
		resp.Body.Close()
	}
}
