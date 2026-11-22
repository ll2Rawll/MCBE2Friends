package main

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

func machineCapacity() (int, uint64) {
	cores := runtime.NumCPU()
	var info syscall.Sysinfo_t
	var memory uint64
	if syscall.Sysinfo(&info) == nil {
		memory = info.Totalram * uint64(info.Unit)
	}
	if cap := readPositive("/sys/fs/cgroup/memory.max"); cap > 0 && (memory == 0 || cap < memory) {
		memory = cap
	}
	if b, err := os.ReadFile("/sys/fs/cgroup/cpu.max"); err == nil {
		f := strings.Fields(string(b))
		if len(f) == 2 {
			q, _ := strconv.Atoi(f[0])
			p, _ := strconv.Atoi(f[1])
			if q > 0 && p > 0 {
				cores = min(cores, max(1, q/p))
			}
		}
	}
	return cores, memory
}

func unsupportedOS() string {
	b, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	m := properties(b)
	if strings.Trim(m["ID"], "\"") != "ubuntu" {
		return ""
	}
	str := strings.Split(strings.Trim(m["VERSION_ID"], "\""), ".")
	major, _ := strconv.Atoi(str[0])
	minor := 0
	if len(str) > 1 {
		minor, _ = strconv.Atoi(str[1])
	}
	if major > 0 && (major < 22 || major == 22 && minor < 4) {
		return "Update to Ubuntu 22.04 LTS or later before purchasing hosting."
	}
	return ""
}
