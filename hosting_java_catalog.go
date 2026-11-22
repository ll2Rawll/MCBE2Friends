package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
)

//go:embed catalog/java.json
var javaCatalogJSON []byte

type pinnedJar struct {
	Name, URL, SHA256 string
	Size              int64
}

func (j pinnedJar) component() javaComponent {
	return javaComponent{asset{j.Name, j.SHA256, j.Size}, j.URL}
}

type javaRelease struct {
	ID           string
	Java         int
	Vanilla      pinnedJar
	Paper        *pinnedJar
	PaperChannel string
	Fabric       []string
}
type javaCatalog struct {
	Loader    string
	Releases  []javaRelease
	Libraries map[string]pinnedJar
}

var catalog = func() javaCatalog {
	var c javaCatalog
	if err := json.Unmarshal(javaCatalogJSON, &c); err != nil {
		panic(err)
	}
	return c
}()

// The protocol spoken by the bundled Geyser build is specific to this Java release.
const geyserGameVersion = "26.3"

func (r javaRelease) supportsGeyser() bool { return r.Paper != nil && r.Java >= 21 }

// Profiles written before version selection was added always ran 26.3.
const legacyJavaVersion = "26.3"

func findJavaRelease(id string) (*javaRelease, error) {
	for i := range catalog.Releases {
		if catalog.Releases[i].ID == id {
			return &catalog.Releases[i], nil
		}
	}
	return nil, errors.New("Choose a supported Minecraft release from the version list.")
}

type javaChoice struct {
	ID           string `json:"id"`
	Java         int    `json:"java"`
	Paper        bool   `json:"paper"`
	Geyser       bool   `json:"geyser"`
	PaperChannel string `json:"paperChannel,omitempty"`
}

func javaChoices() []javaChoice {
	out := make([]javaChoice, 0, len(catalog.Releases))
	for _, r := range catalog.Releases {
		out = append(out, javaChoice{r.ID, r.Java, r.Paper != nil, r.supportsGeyser(), r.PaperChannel})
	}
	return out
}
func (r *javaRelease) runtime() javaRuntime {
	switch r.Java {
	case 17:
		return runtime17
	case 21:
		return broadcastRuntime
	case 25:
		return serverRuntime
	}
	panic("Missing pinned Java runtime for Java " + strconv.Itoa(r.Java))
}
func fabricLibraryPath(dir string, lib pinnedJar) string {
	return filepath.Join(dir, ".mcbe2friends-fabric", lib.SHA256+".jar")
}
func (r *javaRelease) launchArgs(dir string, engine string) []string {
	if engine != "fabric" {
		return []string{"-jar", "server.jar"}
	}
	libs := make([]string, 0, len(r.Fabric))
	for _, coord := range r.Fabric {
		libs = append(libs, fabricLibraryPath(dir, catalog.Libraries[coord]))
	}
	return []string{"-Dfabric.gameJarPath=server.jar", "-cp", strings.Join(libs, string(filepath.ListSeparator)), "net.fabricmc.loader.impl.launch.server.FabricServerLauncher"}
}
