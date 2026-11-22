package main

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func javaHTTP(h *Hosting, w http.ResponseWriter, r *http.Request, path string) {
	reject := func(status int, err error) { writeJSON(w, status, map[string]string{"error": err.Error()}) }
	id, action, _ := strings.Cut(strings.TrimPrefix(path, "/"), "/")
	if id != "" && !javaID.MatchString(id) {
		reject(404, errors.New("Unknown Java server."))
		return
	}
	if !(action == "stop" && r.Method == "POST") {
		if err := h.allowed(false); err != nil {
			reject(403, err)
			return
		}
	}
	if id == "" {
		switch r.Method {
		case "GET":
			lib, err := h.javaLibrary(r.URL.Query().Get("selected"))
			if err != nil {
				reject(400, err)
				return
			}
			writeJSON(w, 200, lib)
		case "POST":
			var p struct {
				Name, Engine string
				Geyser       bool
			}
			if err := hostJSON(w, r, &p); err != nil {
				reject(400, err)
				return
			}
			id, err := h.createJava(p.Name, p.Engine, p.Geyser)
			if err != nil {
				reject(400, err)
				return
			}
			writeJSON(w, 201, map[string]string{"id": id})
		default:
			reject(405, errors.New("Method not allowed."))
		}
		return
	}
	if r.Method == "GET" && action == "files" {
		dir, err := h.javaServer(id)
		if err != nil {
			reject(404, err)
			return
		}
		f, err := listJavaFiles(dir)
		if err != nil {
			reject(400, err)
			return
		}
		writeJSON(w, 200, f)
		return
	}
	if r.Method != "POST" {
		reject(405, errors.New("Method not allowed."))
		return
	}
	var err error
	switch action {
	case "start":
		var p struct{ AcceptTerms bool }
		if err = hostJSON(w, r, &p); err == nil {
			err = h.startJava(id, p.AcceptTerms, nil)
		}
	case "stop":
		err = h.stopJava(id)
	case "settings":
		var c JavaConfig
		if err = hostJSON(w, r, &c); err == nil {
			err = h.javaEdit(id, func(dir string, old JavaConfig) error {
				if c.Engine != old.Engine {
					return errors.New("Create another profile to use a different Java server engine.")
				}
				if e := c.validate(h.hw); e != nil {
					return e
				}
				return c.write(dir)
			})
		}
	case "command":
		var p struct{ Command string }
		if err = hostJSON(w, r, &p); err == nil {
			err = h.javaCommand(id, p.Command)
		}
	case "player":
		var p struct {
			Name            string
			Remove, Bedrock bool
		}
		if err = hostJSON(w, r, &p); err == nil {
			name := strings.TrimSpace(p.Name)
			verb := "add"
			if p.Remove {
				verb = "remove"
			}
			if p.Bedrock {
				dir, e := h.javaServer(id)
				if e != nil {
					err = e
					break
				}
				c, e := h.javaConfig(dir)
				if e != nil {
					err = e
					break
				}
				if !c.Geyser || !xboxPlayer.MatchString(name) {
					err = errors.New("Choose a Geyser server and enter the Xbox gamertag.")
					break
				}
				err = h.javaCommand(id, "fwhitelist "+verb+" "+strconv.Quote(name))
			} else if !javaPlayer.MatchString(name) {
				err = errors.New("Enter a Java username of 1–16 letters, digits, or underscores.")
			} else {
				err = h.javaCommand(id, "whitelist "+verb+" "+name)
			}
		}
	case "plugin":
		var p struct{ File, Action string }
		if err = hostJSON(w, r, &p); err == nil {
			err = h.javaEdit(id, func(dir string, c JavaConfig) error { return changeJavaPlugin(dir, p.File, p.Action, c) })
		}
	case "backup", "restore", "import-world", "import-plugin":
		rc := http.NewResponseController(w)
		_ = rc.SetReadDeadline(time.Now().Add(20 * time.Minute))
		_ = rc.SetWriteDeadline(time.Now().Add(30 * time.Minute))
		err = h.javaEdit(id, func(dir string, c JavaConfig) error {
			if action == "import-world" || action == "import-plugin" {
				if r.Header.Get("Content-Type") != "application/octet-stream" {
					return errors.New("Select a file to import.")
				}
				limit := int64(128 << 20)
				if action == "import-world" {
					limit = 2 << 30
				}
				f, e := os.CreateTemp(dir, ".upload-")
				if e != nil {
					return e
				}
				defer os.Remove(f.Name())
				defer f.Close()
				if _, e = io.Copy(f, http.MaxBytesReader(w, r.Body, limit)); e != nil {
					return errors.New("The file was interrupted or exceeds the upload limit.")
				}
				if e = f.Close(); e != nil {
					return e
				}
				if action == "import-plugin" {
					return importJavaPlugin(dir, f.Name(), c)
				}
				return importJavaWorld(r.Context(), dir, f.Name(), r.URL.Query().Get("name"))
			}
			var p struct{ Name, Backup string }
			if e := hostJSON(w, r, &p); e != nil {
				return e
			}
			if action == "restore" {
				return restoreJavaBackup(r.Context(), dir, p.Backup, p.Name)
			}
			return backupJavaWorld(r.Context(), dir, c.World)
		})
	case "folder":
		var p struct{ Backups bool }
		if err = hostJSON(w, r, &p); err == nil {
			var dir string
			if dir, err = h.javaServer(id); err == nil {
				if p.Backups {
					dir = filepath.Join(dir, "backups")
					err = os.MkdirAll(dir, 0700)
				}
				if err == nil {
					err = openHostFolder(dir)
				}
			}
		}
	default:
		reject(404, errors.New("Unknown Java hosting action."))
		return
	}
	if err != nil {
		reject(400, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

var javaPlayer = regexp.MustCompile("^[A-Za-z0-9_]{1,16}$")
var xboxPlayer = regexp.MustCompile("^[A-Za-z0-9_][A-Za-z0-9_ #.-]{0,31}$")
