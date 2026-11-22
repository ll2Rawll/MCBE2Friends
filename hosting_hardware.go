package main

import (
	"fmt"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
)

type Hardware struct {
	Platform      string  `json:"platform"`
	Cores         int     `json:"cores"`
	RAMGiB        float64 `json:"ramGiB"`
	JavaSupported bool    `json:"javaSupported"`
	Supported     bool    `json:"supported"`
	Note          string  `json:"note"`
	Tier          string  `json:"tier"`
}

func recommended(cores int, gib float64) (tier string, players, view, tick, threads int) {
	switch {
	case cores >= 8 && gib >= 15.5:
		return "Large", 12, 16, 6, min(cores-2, 8)
	case cores >= 4 && gib >= 7.5:
		return "Balanced", 8, 12, 4, min(cores-1, 4)
	default:
		return "Small", 4, 8, 4, max(1, min(cores-1, 2))
	}
}
func hardware() Hardware {
	cores, mem := machineCapacity()
	gib := float64(mem) / (1 << 30)
	tier, _, _, _, _ := recommended(cores, gib)
	label := map[string]string{"windows": "Windows", "linux": "Linux", "darwin": "macOS"}[runtime.GOOS]
	arch := map[string]string{"amd64": "x64", "arm64": "ARM64"}[runtime.GOARCH]
	h := Hardware{Platform: strings.TrimSpace(label + " " + arch), Cores: cores, RAMGiB: gib, Supported: runtime.GOARCH == "amd64" && (runtime.GOOS == "windows" || runtime.GOOS == "linux"), Tier: tier}
	_, hasRuntime := serverRuntime.assets[runtime.GOOS+"/"+runtime.GOARCH]
	h.JavaSupported = hasRuntime && (mem == 0 || gib >= 3.5)
	switch {
	case !h.Supported && h.JavaSupported:
		h.Note = "This computer supports Java servers, including Geyser cross-play. Native Bedrock requires Windows or Linux x64."
	case !h.Supported:
		h.Note = "Native Bedrock hosting requires a Windows or Linux x64 PC. This computer can still broadcast an existing server for free."
	case mem > 0 && gib < 3.5:
		h.Supported = false
		h.Note = "Mojang lists 4 GB of system RAM. Use a PC with more memory for hosting."
	case runtime.GOOS == "linux":
		h.Note = "Mojang supports Ubuntu 22.04 LTS or later. Other x64 Linux distributions depend on their installed system libraries."
	default:
		h.Note = "Native Bedrock and Java hosting are available on this PC."
	}
	if h.Supported {
		if reason := unsupportedOS(); reason != "" {
			h.Supported = false
			h.Note = reason
		}
	}
	return h
}
func lanAddress() string {
	addrs, _ := net.InterfaceAddrs()
	for _, addr := range addrs {
		if n, ok := addr.(*net.IPNet); ok && n.IP.To4() != nil && n.IP.IsPrivate() && !n.IP.IsLoopback() {
			return n.IP.String()
		}
	}
	return ""
}
func readPositive(path string) uint64 {
	b, _ := os.ReadFile(path)
	n, _ := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	return n
}
func (hw Hardware) defaults() HostConfig {
	_, players, view, tick, threads := recommended(hw.Cores, hw.RAMGiB)
	return HostConfig{Name: "Mcbe2Friends", World: "My World", Port: 19132, Players: players, View: view, Tick: tick, Threads: threads, Mode: "survival", Difficulty: "easy", AllowList: true}
}
func supportedNote(hw Hardware) error {
	if !hw.Supported {
		return fmt.Errorf("%s", hw.Note)
	}
	return nil
}
