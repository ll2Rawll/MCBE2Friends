package main

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

type modListing struct {
	Managed []installedMod `json:"managed"`
	Manual  []string       `json:"manual"`
}

func (h *Hosting) modList(id, dir string) (modListing, error) {
	h.ops.Lock()
	defer h.ops.Unlock()
	var out modListing
	if !h.modWork[id] {
		if err := recoverMods(dir); err != nil {
			return out, err
		}
	}
	s, err := getModState(dir)
	if err != nil {
		return out, err
	}
	out.Managed = slices.SortedFunc(maps.Values(s.Items), func(a, b installedMod) int {
		return strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title))
	})
	out.Manual, err = unmanagedMods(filepath.Join(dir, "mods"), s.Items)
	return out, err
}
func modHTTP(h *Hosting, w http.ResponseWriter, r *http.Request, id, action string) {
	fail := func(status int, err error) { writeJSON(w, status, map[string]string{"error": err.Error()}) }
	dir, err := h.javaServer(id)
	if err != nil {
		fail(404, err)
		return
	}
	c, err := h.javaConfig(dir)
	if err != nil {
		fail(400, err)
		return
	}
	if c.Engine != "fabric" {
		fail(400, errors.New("Choose a Fabric server to use the Modrinth browser."))
		return
	}
	ctx := r.Context()
	var data any = map[string]bool{"ok": true}
	switch {
	case r.Method == "GET" && action == "mods":
		data, err = h.modList(id, dir)
	case r.Method == "GET" && action == "mods/search":
		query := r.URL.Query()
		offset := 0
		if val := query.Get("offset"); val != "" {
			offset, err = strconv.Atoi(val)
		}
		if err == nil {
			data, err = modrinth.search(ctx, c.Version, strings.TrimSpace(query.Get("q")), offset)
		}
	case r.Method == "GET" && action == "mods/versions":
		project := r.URL.Query().Get("project")
		if _, err = modrinth.project(ctx, project); err == nil {
			var list []modVersion
			list, err = modrinth.versions(ctx, project, c.Version)
			choices := make([]modChoice, 0, len(list))
			for _, v := range list {
				choices = append(choices, modChoice{v.ID, v.Number, v.Kind})
			}
			data = choices
		}
	case r.Method == "POST" && action == "mods/folder":
		if err = os.MkdirAll(filepath.Join(dir, "mods"), 0700); err == nil {
			err = openHostFolder(filepath.Join(dir, "mods"))
		}
	case r.Method == "POST" && action == "mods/change":
		var q struct{ Project, Version, Action string }
		if err = hostJSON(w, r, &q); err == nil {
			_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(16 * time.Minute))
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, 15*time.Minute)
			defer cancel()
			err = h.updateMods(ctx, id, q.Project, q.Version, q.Action)
		}
	default:
		fail(405, errors.New("Unknown mod action or method."))
		return
	}
	if err != nil {
		fail(400, err)
		return
	}
	writeJSON(w, 200, data)
}
