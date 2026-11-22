package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type HostConfig struct {
	Name       string `json:"name"`
	World      string `json:"world"`
	Port       int    `json:"port"`
	Players    int    `json:"players"`
	View       int    `json:"view"`
	Tick       int    `json:"tick"`
	Threads    int    `json:"threads"`
	Mode       string `json:"mode"`
	Difficulty string `json:"difficulty"`
	Cheats     bool   `json:"cheats"`
	AllowList  bool   `json:"allowList"`
}

func worldNameOK(s string) bool {
	if s == "" || strings.TrimSpace(s) != s || utf8.RuneCountInString(s) > 60 || strings.HasPrefix(s, ".") || strings.HasSuffix(s, ".") || strings.ContainsAny(s, "<>:\"/\\|?*") || strings.IndexFunc(s, unicode.IsControl) >= 0 || !filepath.IsLocal(s) {
		return false
	}
	stem := strings.ToUpper(strings.Split(s, ".")[0])
	if slices.Contains([]string{"CON", "NUL", "AUX", "PRN"}, stem) {
		return false
	}
	return !(len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '0' && stem[3] <= '9')
}
func (c HostConfig) validate() error {
	if !worldNameOK(c.World) {
		return errors.New("Use a world name of 1–60 characters without slashes or reserved filename characters.")
	}
	if strings.TrimSpace(c.Name) == "" || utf8.RuneCountInString(c.Name) > 64 || strings.IndexFunc(c.Name, unicode.IsControl) >= 0 {
		return errors.New("Choose a server name of 1–64 characters on one line.")
	}
	if c.Port < 1 || c.Port > 65535 || c.Players < 1 || c.Players > 1000 || c.View < 5 || c.View > 96 || c.Tick < 4 || c.Tick > 12 || c.Threads < 0 || c.Threads > 256 {
		return errors.New("Check the limits shown next to the server settings.")
	}
	if !slices.Contains([]string{"survival", "creative", "adventure"}, c.Mode) || !slices.Contains([]string{"peaceful", "easy", "normal", "hard"}, c.Difficulty) {
		return errors.New("Choose a Bedrock game mode and difficulty.")
	}
	return nil
}

func properties(data []byte) map[string]string {
	m := map[string]string{}
	for _, l := range strings.Split(string(data), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		k, v, ok := strings.Cut(l, "=")
		if ok {
			m[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return m
}
func (c HostConfig) fromProperties(m map[string]string) HostConfig {
	for k, d := range map[string]*string{"server-name": &c.Name, "level-name": &c.World, "gamemode": &c.Mode, "difficulty": &c.Difficulty} {
		if v, ok := m[k]; ok {
			*d = v
		}
	}
	for k, d := range map[string]*int{"server-port": &c.Port, "max-players": &c.Players, "view-distance": &c.View, "tick-distance": &c.Tick, "max-threads": &c.Threads} {
		if n, e := strconv.Atoi(m[k]); e == nil {
			*d = n
		}
	}
	if v, ok := m["allow-cheats"]; ok {
		c.Cheats = v == "true"
	}
	if v, ok := m["allow-list"]; ok {
		c.AllowList = v == "true"
	}
	return c
}
func (c HostConfig) apply(path string) error {
	if err := c.validate(); err != nil {
		return err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	old := properties(b)
	values := map[string]string{"server-name": c.Name, "level-name": c.World, "server-port": strconv.Itoa(c.Port), "max-players": strconv.Itoa(c.Players), "view-distance": strconv.Itoa(c.View), "tick-distance": strconv.Itoa(c.Tick), "max-threads": strconv.Itoa(c.Threads), "difficulty": c.Difficulty, "gamemode": c.Mode, "allow-cheats": strconv.FormatBool(c.Cheats), "allow-list": strconv.FormatBool(c.AllowList)}
	if old["server-udp-ports"] == old["server-port"] {
		values["server-udp-ports"] = strconv.Itoa(c.Port)
	}
	sep := "\n"
	if strings.Contains(string(b), "\r\n") {
		sep = "\r\n"
	}
	lines := strings.Split(strings.TrimRight(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n"), "\n")
	seen := map[string]bool{}
	for i, line := range lines {
		k, _, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if v, exists := values[k]; exists {
			lines[i] = k + "=" + v
			seen[k] = true
		}
	}
	var missing []string
	for k := range values {
		if !seen[k] {
			missing = append(missing, k)
		}
	}
	sort.Strings(missing)
	for _, k := range missing {
		lines = append(lines, k+"="+values[k])
	}
	return atomicWrite(path, []byte(strings.Join(lines, sep)+sep))
}
func (h *Hosting) config() (HostConfig, error) {
	c := h.hw.defaults()
	b, err := os.ReadFile(filepath.Join(h.server, "server.properties"))
	if err == nil {
		return c.fromProperties(properties(b)), nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return c, err
	}
	b, err = os.ReadFile(filepath.Join(h.root, "preferences.json"))
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err == nil {
		err = json.Unmarshal(b, &c)
	}
	return c, err
}
func (h *Hosting) saveConfig(c HostConfig) error {
	if err := c.validate(); err != nil {
		return err
	}
	if h.installed() {
		return c.apply(filepath.Join(h.server, "server.properties"))
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(h.root, "preferences.json"), b)
}
func (h *Hosting) worldPath(name string) (string, error) {
	if !worldNameOK(name) {
		return "", errors.New("Choose a valid world folder.")
	}
	dir := filepath.Join(h.server, "worlds", name)
	for _, p := range []string{filepath.Join(h.server, "worlds"), dir} {
		info, err := os.Lstat(p)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("World folders must be ordinary directories: %s", filepath.Base(p))
		}
	}
	return dir, nil
}
