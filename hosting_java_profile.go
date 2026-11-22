package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	javaprops "github.com/magiconair/properties"
)

var javaVersion = catalog.Releases[0].ID

var javaID = regexp.MustCompile("^[a-f0-9]{24}$")

type JavaConfig struct {
	Version     string `json:"version"`
	Name        string `json:"name"`
	Engine      string `json:"engine"`
	Geyser      bool   `json:"geyser"`
	World       string `json:"world"`
	Port        int    `json:"port"`
	BedrockPort int    `json:"bedrockPort"`
	Memory      int    `json:"memory"`
	Players     int    `json:"players"`
	View        int    `json:"view"`
	Simulation  int    `json:"simulation"`
	Mode        string `json:"mode"`
	Difficulty  string `json:"difficulty"`
	Whitelist   bool   `json:"whitelist"`
}

func (hw Hardware) heapBudget() int {
	if hw.RAMGiB <= 0 {
		return 2048
	}
	return min(32768, int(hw.RAMGiB*1024*0.60)/128*128)
}
func (hw Hardware) javaDefaults() JavaConfig {
	_, players, view, _, _ := recommended(hw.Cores, hw.RAMGiB)
	memory := 1536
	if hw.RAMGiB >= 7.5 {
		memory = 3072
	}
	if hw.RAMGiB >= 15.5 {
		memory = 4096
	}
	return JavaConfig{Version: javaVersion, Name: "My Java server", Engine: "vanilla", World: "world", Port: 25565, BedrockPort: 19132, Memory: min(memory, hw.heapBudget()), Players: players, View: min(view, 12), Simulation: min(6, view), Mode: "survival", Difficulty: "normal"}
}
func (c JavaConfig) validate(hw Hardware) error {
	release, err := findJavaRelease(c.Version)
	if err != nil {
		return err
	}
	switch c.Engine {
	case "vanilla", "fabric":
	case "paper":
		if release.Paper == nil {
			return fmt.Errorf("Paper has no published build for Minecraft %s.", c.Version)
		}
	default:
		return errors.New("Choose Vanilla, Fabric, or Paper.")
	}
	if c.Geyser && (c.Engine != "paper" || !release.supportsGeyser()) {
		return errors.New("Managed Geyser cross-play needs a published Paper build for 1.20.5 or later.")
	}
	if strings.TrimSpace(c.Name) == "" || utf8.RuneCountInString(c.Name) > 64 || strings.IndexFunc(c.Name, unicode.IsControl) >= 0 {
		return errors.New("Enter a server name of 1–64 characters on one line.")
	}
	if !worldNameOK(c.World) {
		return errors.New("Choose a valid world folder name of 1–60 characters.")
	}
	if c.Port < 1 || c.Port > 65535 || c.BedrockPort < 1 || c.BedrockPort > 65535 || c.Geyser && c.Port == c.BedrockPort {
		return errors.New("Use ports from 1 to 65535. Java and Geyser need different ports.")
	}
	if c.Memory < 1024 || c.Memory > hw.heapBudget() {
		return fmt.Errorf("Choose a Java memory limit between 1024 and %d MiB for this PC.", hw.heapBudget())
	}
	if c.Players < 1 || c.Players > 1000 || c.View < 2 || c.View > 32 || c.Simulation < 2 || c.Simulation > 32 {
		return errors.New("Use 1–1000 player slots and 2–32 chunks for each distance.")
	}
	if !slices.Contains([]string{"survival", "creative", "adventure", "spectator"}, c.Mode) || !slices.Contains([]string{"peaceful", "easy", "normal", "hard"}, c.Difficulty) {
		return errors.New("Choose a Java game mode and difficulty.")
	}
	return nil
}

func (h *Hosting) javaRoot() string { return filepath.Join(h.root, "java", "servers") }
func (h *Hosting) javaServer(id string) (string, error) {
	if !javaID.MatchString(id) {
		return "", errors.New("Choose a Java server.")
	}
	dir := filepath.Join(h.javaRoot(), id)
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		return "", errors.New("This Java server folder is unavailable.")
	}
	return dir, nil
}
func readJavaProperties(dir string) (*javaprops.Properties, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "server.properties"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	loader := javaprops.Loader{Encoding: javaprops.UTF8, DisableExpansion: true}
	return loader.LoadBytes(raw)
}
func (h *Hosting) javaConfig(dir string) (JavaConfig, error) {
	c := h.hw.javaDefaults()
	p, err := readJavaProperties(dir)
	if err != nil {
		return c, err
	}
	if _, ok := p.Get("mcbe2friends-engine"); !ok {
		return c, errors.New("This profile is missing its Mcbe2Friends settings in server.properties.")
	}
	c.Version = p.GetString("mcbe2friends-version", legacyJavaVersion)
	c.Name = p.GetString("motd", c.Name)
	c.World = p.GetString("level-name", c.World)
	c.Engine = p.GetString("mcbe2friends-engine", "")
	c.Geyser = p.GetBool("mcbe2friends-geyser", false)
	c.Memory = p.GetInt("mcbe2friends-memory-mib", c.Memory)
	c.BedrockPort = p.GetInt("mcbe2friends-bedrock-port", c.BedrockPort)
	c.Port = p.GetInt("server-port", c.Port)
	c.Players = p.GetInt("max-players", c.Players)
	c.View = p.GetInt("view-distance", c.View)
	c.Simulation = p.GetInt("simulation-distance", c.Simulation)
	c.Mode = p.GetString("gamemode", c.Mode)
	c.Difficulty = p.GetString("difficulty", c.Difficulty)
	c.Whitelist = p.GetBool("white-list", false)
	return c, nil
}
func (c JavaConfig) write(dir string) error {
	p, err := readJavaProperties(dir)
	if err != nil {
		return err
	}
	values := map[string]string{
		"mcbe2friends-version": c.Version, "mcbe2friends-engine": c.Engine, "mcbe2friends-geyser": strconv.FormatBool(c.Geyser),
		"mcbe2friends-memory-mib": strconv.Itoa(c.Memory), "mcbe2friends-bedrock-port": strconv.Itoa(c.BedrockPort),
		"motd": c.Name, "level-name": c.World, "server-port": strconv.Itoa(c.Port),
		"gamemode": c.Mode, "difficulty": c.Difficulty, "white-list": strconv.FormatBool(c.Whitelist),
		"max-players": strconv.Itoa(c.Players), "view-distance": strconv.Itoa(c.View), "simulation-distance": strconv.Itoa(c.Simulation),
		"online-mode": "true", "enable-status": "true",
	}
	// Bedrock clients cannot send Java-signed chat. Remember the owner's
	// Java-only policy and restore it when they switch cross-play off.
	const previous = "mcbe2friends-java-secure-profile"
	saved, recorded := p.Get(previous)
	if c.Geyser {
		if !recorded {
			values[previous] = p.GetString("enforce-secure-profile", "true")
		}
		values["enforce-secure-profile"] = "false"
	} else if recorded {
		values["enforce-secure-profile"] = saved
		p.Delete(previous)
	}
	for _, k := range sortedKeys(values) {
		if _, _, err = p.Set(k, values[k]); err != nil {
			return err
		}
	}
	p.WriteSeparator = "="
	var buf bytes.Buffer
	if _, err = p.WriteComment(&buf, "# ", javaprops.UTF8); err != nil {
		return err
	}
	return atomicWrite(filepath.Join(dir, "server.properties"), buf.Bytes())
}
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

type javaProfile struct {
	ID     string     `json:"id"`
	Config JavaConfig `json:"config"`
	javaStatus
	Terms bool `json:"terms"`
}
type javaLibrary struct {
	Versions    []javaChoice  `json:"versions"`
	Loader      string        `json:"loader"`
	Profiles    []javaProfile `json:"profiles"`
	Recommended JavaConfig    `json:"recommended"`
	MaxMemory   int           `json:"maxMemory"`
	Version     string        `json:"version"`
}

func (h *Hosting) javaLibrary(selected string) (javaLibrary, error) {
	lib := javaLibrary{Versions: javaChoices(), Loader: catalog.Loader, Recommended: h.hw.javaDefaults(), MaxMemory: h.hw.heapBudget(), Version: javaVersion}
	if err := h.allowed(false); err != nil {
		return lib, err
	}
	entries, err := os.ReadDir(h.javaRoot())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return lib, err
	}
	used := map[int]bool{}
	if h.hw.Supported {
		if b, err := h.config(); err == nil {
			used[b.Port] = true
		}
	}
	for _, e := range entries {
		if !e.IsDir() || !javaID.MatchString(e.Name()) {
			continue
		}
		dir := filepath.Join(h.javaRoot(), e.Name())
		c, err := h.javaConfig(dir)
		h.mu.Lock()
		state := h.javaStateLocked(e.Name()).snapshot(selected == e.Name())
		h.mu.Unlock()
		if err != nil {
			state.Message = err.Error()
			state.Phase = "error"
			c.Name = "Unreadable server · " + e.Name()[:6]
		}
		used[c.Port] = true
		if c.Geyser {
			used[c.BedrockPort] = true
		}
		lib.Profiles = append(lib.Profiles, javaProfile{e.Name(), c, state, javaTerms(dir)})
	}
	sort.SliceStable(lib.Profiles, func(i, j int) bool {
		return strings.ToLower(lib.Profiles[i].Config.Name) < strings.ToLower(lib.Profiles[j].Config.Name)
	})
	for used[lib.Recommended.Port] {
		lib.Recommended.Port++
	}
	for used[lib.Recommended.BedrockPort] || lib.Recommended.BedrockPort == lib.Recommended.Port {
		lib.Recommended.BedrockPort++
	}
	return lib, nil
}
func javaTerms(dir string) bool {
	data, _ := os.ReadFile(filepath.Join(dir, "eula.txt"))
	return properties(data)["eula"] == "true"
}
func (h *Hosting) createJava(name, engine string, geyser bool, version string) (string, error) {
	h.ops.Lock()
	defer h.ops.Unlock()
	if !h.hw.JavaSupported {
		return "", errors.New("Java hosting needs a supported computer with at least 4 GB of RAM.")
	}
	lib, err := h.javaLibrary("")
	if err != nil {
		return "", err
	}
	c := lib.Recommended
	if version != "" {
		c.Version = version
	}
	c.Name, c.Engine, c.Geyser = strings.TrimSpace(name), engine, geyser
	if err = c.validate(h.hw); err != nil {
		return "", err
	}
	root := h.javaRoot()
	if err = os.MkdirAll(root, 0700); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(root, ".new-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	if err = c.write(tmp); err != nil {
		return "", err
	}
	var random [12]byte
	if _, err = rand.Read(random[:]); err != nil {
		return "", err
	}
	id := hex.EncodeToString(random[:])
	return id, os.Rename(tmp, filepath.Join(root, id))
}
func (h *Hosting) javaEdit(id string, fn func(dir string, c JavaConfig) error) error {
	h.ops.Lock()
	defer h.ops.Unlock()
	if h.modWork[id] {
		return errors.New("Wait for the mod changes on this server to finish.")
	}
	if err := h.allowed(false); err != nil {
		return err
	}
	h.mu.Lock()
	busy := h.javaStateLocked(id).active != nil
	h.mu.Unlock()
	if busy {
		return errors.New("Stop this Java server before changing its files.")
	}
	dir, err := h.javaServer(id)
	if err != nil {
		return err
	}
	c, err := h.javaConfig(dir)
	if err != nil {
		return err
	}
	return fn(dir, c)
}
