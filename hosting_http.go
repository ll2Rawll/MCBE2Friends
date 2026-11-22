package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode"
)

func hostJSON(w http.ResponseWriter, r *http.Request, v any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return errors.New("Send this action as JSON.")
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return errors.New("The launcher could not read these settings.")
	}
	if e := d.Decode(new(any)); !errors.Is(e, io.EOF) {
		return errors.New("Send a single settings object.")
	}
	return nil
}
func (h *Hosting) upload(w http.ResponseWriter, r *http.Request, world bool) error {
	if !h.installed() {
		return errors.New("Start the initial server install first.")
	}
	if r.Header.Get("Content-Type") != "application/octet-stream" {
		return errors.New("Choose a Bedrock archive.")
	}
	limit := int64(128 << 20)
	if world {
		limit = 512 << 20
	}
	f, err := os.CreateTemp(h.root, ".upload-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err = io.Copy(f, http.MaxBytesReader(w, r.Body, limit)); err != nil {
		return errors.New("The upload was interrupted or exceeded the file size limit.")
	}
	if err = f.Close(); err != nil {
		return err
	}
	if world {
		return h.importWorld(r.Context(), f.Name(), r.URL.Query().Get("name"))
	}
	return h.importPacks(r.Context(), f.Name())
}
func hostingHTTP(h *Hosting, w http.ResponseWriter, r *http.Request) {
	reject := func(code int, e error) { writeJSON(w, code, map[string]string{"error": e.Error()}) }
	action := strings.TrimPrefix(r.URL.Path, "/api/hosting")
	if r.Method == "GET" && action == "" {
		writeJSON(w, 200, h.status())
		return
	}
	if r.Method == "GET" && action == "/files" {
		if err := h.allowed(false); err != nil {
			reject(403, err)
			return
		}
		data, e := h.files()
		if e != nil {
			reject(400, e)
			return
		}
		writeJSON(w, 200, data)
		return
	}
	if r.Method != "POST" {
		reject(405, errors.New("Method not allowed."))
		return
	}
	if action != "/activate" {
		if err := h.allowed(false); err != nil {
			reject(403, err)
			return
		}
	}
	var err error
	switch action {
	case "/activate":
		var p struct {
			Key string `json:"key"`
		}
		if err = hostJSON(w, r, &p); err == nil {
			err = h.activate(r.Context(), p.Key)
		}
	case "/start":
		var p struct {
			AcceptTerms bool `json:"acceptTerms"`
		}
		if err = hostJSON(w, r, &p); err == nil {
			err = h.start(p.AcceptTerms)
		}
	case "/stop":
		h.stop(false)
	case "/settings":
		var c HostConfig
		if err = hostJSON(w, r, &c); err == nil {
			err = h.edit(func() error { return h.saveConfig(c) })
		}
	case "/pack":
		var p struct {
			ID              string
			Enabled, Remove bool
		}
		if err = hostJSON(w, r, &p); err == nil {
			err = h.edit(func() error { return h.changePack(p.ID, p.Enabled, p.Remove) })
		}
	case "/import-pack", "/import-world", "/backup":
		controller := http.NewResponseController(w)
		_ = controller.SetReadDeadline(time.Now().Add(10 * time.Minute))
		_ = controller.SetWriteDeadline(time.Now().Add(20 * time.Minute))
		err = h.edit(func() error {
			if action != "/backup" {
				return h.upload(w, r, action == "/import-world")
			}
			var p struct{ Name string }
			if e := hostJSON(w, r, &p); e != nil {
				return e
			}
			return h.backupWorld(r.Context(), p.Name)
		})
	case "/command":
		var p struct{ Command string }
		if err = hostJSON(w, r, &p); err == nil {
			err = h.command(p.Command)
		}
	case "/player":
		var p struct {
			Name   string
			Remove bool
		}
		if err = hostJSON(w, r, &p); err == nil {
			name := strings.TrimSpace(p.Name)
			if name == "" || len(name) > 64 || strings.ContainsAny(name, "\"\\") || strings.IndexFunc(name, unicode.IsControl) >= 0 {
				err = errors.New("Enter the player's Xbox gamertag on one line.")
			} else {
				verb := "add"
				if p.Remove {
					verb = "remove"
				}
				err = h.command("allowlist " + verb + " \"" + name + "\"")
			}
		}
	case "/folder":
		var p struct{ Backups bool }
		if err = hostJSON(w, r, &p); err == nil {
			err = h.openFolder(p.Backups)
		}
	default:
		reject(404, errors.New("Unknown hosting action."))
		return
	}
	if err != nil {
		reject(400, err)
		return
	}
	writeJSON(w, 200, h.status())
}
