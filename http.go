package main

import (
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed web
var webFiles embed.FS

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func handler(l *Launcher, i Instance, quit func()) http.Handler {
	root, _ := fs.Sub(webFiles, "web")
	static := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		if "http://"+r.Host != i.origin() {
			http.Error(w, "Invalid launcher address.", http.StatusForbidden)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			if r.Method != "GET" && r.Method != "HEAD" {
				http.Error(w, "Method not allowed.", 405)
				return
			}
			static.ServeHTTP(w, r)
			return
		}
		reject := func(status int, msg string) { writeJSON(w, status, map[string]string{"error": msg}) }
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+i.Token)) != 1 {
			reject(401, "Reopen Mcbe2Friends on this computer to reconnect the launcher.")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != i.origin() {
			reject(403, "Open this action in the local launcher.")
			return
		}
		if r.Method == "GET" && r.URL.Path == "/api/state" {
			writeJSON(w, 200, l.snapshot())
			return
		}
		if r.Method != "POST" {
			reject(405, "Method not allowed.")
			return
		}
		var err error
		switch r.URL.Path {
		case "/api/settings":
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				reject(415, "Send the settings as JSON.")
				return
			}
			var s Settings
			dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
			dec.DisallowUnknownFields()
			if e := dec.Decode(&s); e != nil {
				reject(400, "The server details could not be read.")
				return
			}
			if e := dec.Decode(new(any)); !errors.Is(e, io.EOF) {
				reject(400, "Send one set of server details.")
				return
			}
			err = l.save(s)
		case "/api/start":
			err = l.start()
		case "/api/stop":
			l.stop(false)
		case "/api/forget":
			err = l.forgetAccount()
		case "/api/quit":
			writeJSON(w, 200, map[string]bool{"quitting": true})
			quit()
			return
		default:
			reject(404, "Unknown launcher action.")
			return
		}
		if err != nil {
			reject(400, err.Error())
			return
		}
		writeJSON(w, 200, l.snapshot())
	})
}
