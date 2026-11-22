package main

import (
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

func machineCapacity() (int, uint64) {
	b, _ := exec.Command("/usr/sbin/sysctl", "-n", "hw.memsize").Output()
	n, _ := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	return runtime.NumCPU(), n
}

func unsupportedOS() string { return "" }
