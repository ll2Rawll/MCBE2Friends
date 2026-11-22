package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const modPageSize = 10

var modID = regexp.MustCompile("^[A-Za-z0-9]{1,64}$")
var modrinth = modrinthAPI{"https://api.modrinth.com/v2", &http.Client{Timeout: 30 * time.Second}}

type modrinthAPI struct {
	base   string
	client *http.Client
}

func (a modrinthAPI) get(ctx context.Context, endpoint string, query url.Values, target any) error {
	u := a.base + endpoint
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "ll2Rawll/Mcbe2Friends/1.5 (https://github.com/ll2Rawll/MCBE2Friends)")
	res, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("Could not reach Modrinth: %w", err)
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusTooManyRequests:
		return errors.New("Modrinth is receiving too many requests. Try again shortly.")
	case http.StatusNotFound:
		return errors.New("This Modrinth project or version is no longer available.")
	default:
		return fmt.Errorf("Modrinth returned HTTP %d. Try again shortly.", res.StatusCode)
	}
	const limit = 12 << 20
	data, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return err
	}
	if len(data) > limit {
		return errors.New("This Modrinth response exceeded the size limit.")
	}
	if err = json.Unmarshal(data, target); err != nil {
		return errors.New("Modrinth returned unreadable project data.")
	}
	return nil
}

type modHit struct {
	ID          string `json:"project_id"`
	Title       string `json:"title"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Author      string `json:"author"`
}
type modResults struct {
	Hits  []modHit `json:"hits"`
	Total int      `json:"total_hits"`
}

func (a modrinthAPI) search(ctx context.Context, game, term string, offset int) (modResults, error) {
	var result modResults
	if utf8.RuneCountInString(term) > 100 || offset < 0 || offset > 10000 {
		return result, errors.New("Use a shorter search or choose a valid results page.")
	}
	facets, _ := json.Marshal([][]string{{"project_type:mod"}, {"categories:fabric"}, {"versions:" + game}, {"server_side:required", "server_side:optional"}})
	q := url.Values{"query": {term}, "facets": {string(facets)}, "limit": {strconv.Itoa(modPageSize)}, "offset": {strconv.Itoa(offset)}}
	err := a.get(ctx, "/search", q, &result)
	return result, err
}

type modProject struct {
	ID, Title, Slug string
	Type            string `json:"project_type"`
	Server          string `json:"server_side"`
}
type modFile struct {
	URL, Filename string
	Size          int64
	Primary       bool
	FileType      string `json:"file_type"`
	Hashes        struct{ SHA512 string }
}
type modDependency struct {
	Project string `json:"project_id"`
	Version string `json:"version_id"`
	Type    string `json:"dependency_type"`
}
type modVersion struct {
	ID                  string `json:"id"`
	Project             string `json:"project_id"`
	Number              string `json:"version_number"`
	Kind                string `json:"version_type"`
	Date                string `json:"date_published"`
	Status, Environment string
	Games               []string `json:"game_versions"`
	Loaders             []string
	Files               []modFile
	Dependencies        []modDependency
}
type modChoice struct {
	ID     string `json:"id"`
	Number string `json:"number"`
	Kind   string `json:"kind"`
}

func (v modVersion) compatible(game string) bool {
	if !modID.MatchString(v.ID) || !modID.MatchString(v.Project) || !slices.Contains(v.Games, game) || !slices.Contains(v.Loaders, "fabric") {
		return false
	}
	if strings.HasPrefix(v.Environment, "client_only") || v.Environment == "singleplayer_only" {
		return false
	}
	return v.Status == "listed" || v.Status == "archived" || v.Status == "unlisted"
}
func modJarName(name string) bool {
	if !javaFileName(name) || len(name) > 240 || !strings.HasSuffix(strings.ToLower(name), ".jar") {
		return false
	}
	return !strings.ContainsAny(name, "<>\"|?*") && strings.IndexFunc(name, unicode.IsControl) < 0
}
func (f modFile) serverJar(project string) bool {
	u, err := url.Parse(f.URL)
	if err != nil || u.Scheme != "https" || u.Host != "cdn.modrinth.com" || u.User != nil {
		return false
	}
	if !strings.HasPrefix(u.Path, "/data/"+project+"/") || !strings.HasSuffix(strings.ToLower(path.Base(u.Path)), ".jar") {
		return false
	}
	digest, err := hex.DecodeString(f.Hashes.SHA512)
	return err == nil && len(digest) == 64 && modJarName(f.Filename) && f.FileType == "" && f.Size > 0 && f.Size <= 256<<20
}

func (v modVersion) binary() (modFile, error) {
	var files []modFile
	for _, f := range v.Files {
		if !f.serverJar(v.Project) {
			continue
		}
		if f.Primary {
			return f, nil
		}
		files = append(files, f)
	}
	if len(files) == 1 {
		return files[0], nil
	}
	return modFile{}, errors.New("This version has no unambiguous downloadable Fabric mod JAR.")
}
func (a modrinthAPI) project(ctx context.Context, id string) (modProject, error) {
	var p modProject
	if !modID.MatchString(id) {
		return p, errors.New("Choose a Modrinth project.")
	}
	if err := a.get(ctx, "/project/"+id, nil, &p); err != nil {
		return p, err
	}
	if p.ID != id || p.Type != "mod" || (p.Server != "required" && p.Server != "optional") {
		return p, errors.New("This project is unavailable for dedicated Fabric servers.")
	}
	return p, nil
}
func (a modrinthAPI) versions(ctx context.Context, id, game string) ([]modVersion, error) {
	if !modID.MatchString(id) {
		return nil, errors.New("Choose a Modrinth project.")
	}
	games, _ := json.Marshal([]string{game})
	var list []modVersion
	err := a.get(ctx, "/project/"+id+"/version", url.Values{"loaders": {`["fabric"]`}, "game_versions": {string(games)}}, &list)
	if err != nil {
		return nil, err
	}
	list = slices.DeleteFunc(list, func(v modVersion) bool { _, e := v.binary(); return v.Project != id || !v.compatible(game) || e != nil })
	// Prefer a stable release for automatic dependencies; keep other builds selectable.
	sort.SliceStable(list, func(i, j int) bool {
		if (list[i].Kind == "release") != (list[j].Kind == "release") {
			return list[i].Kind == "release"
		}
		return list[i].Date > list[j].Date
	})
	return list, nil
}

type modResolver struct {
	api      modrinthAPI
	game     string
	versions map[string]modVersion
	projects map[string]modProject
	choices  map[string][]modVersion
}

func (r *modResolver) version(ctx context.Context, id string) (modVersion, error) {
	if v, ok := r.versions[id]; ok {
		return v, nil
	}
	var v modVersion
	if !modID.MatchString(id) {
		return v, errors.New("Choose a Modrinth file version.")
	}
	if err := r.api.get(ctx, "/version/"+id, nil, &v); err != nil {
		return v, err
	}
	if v.ID != id || !v.compatible(r.game) {
		return v, fmt.Errorf("File %s is incompatible with Fabric for Minecraft %s.", id, r.game)
	}
	r.versions[id] = v
	return v, nil
}
func (r *modResolver) choose(ctx context.Context, project, version string) (modVersion, error) {
	if version != "" {
		v, err := r.version(ctx, version)
		if err == nil && v.Project != project {
			err = errors.New("This file belongs to a different Modrinth project.")
		}
		return v, err
	}
	list, ok := r.choices[project]
	if !ok {
		var err error
		list, err = r.api.versions(ctx, project, r.game)
		if err != nil {
			return modVersion{}, err
		}
		r.choices[project] = list
	}
	if len(list) == 0 {
		return modVersion{}, fmt.Errorf("A required project (%s) has no Fabric file for Minecraft %s.", project, r.game)
	}
	r.versions[list[0].ID] = list[0]
	return list[0], nil
}
