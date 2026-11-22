package main

import (
	"runtime"
	"syscall"
	"unsafe"
)

func machineCapacity() (int, uint64) {
	var status struct {
		Length, Load                                                                         uint32
		Total, Available, PageTotal, PageAvailable, VirtualTotal, VirtualAvailable, Extended uint64
	}
	status.Length = uint32(unsafe.Sizeof(status))
	ok, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx").Call(uintptr(unsafe.Pointer(&status)))
	if ok == 0 {
		return runtime.NumCPU(), 0
	}
	return runtime.NumCPU(), status.Total
}

func unsupportedOS() string {
	var info struct {
		Size, Major, Minor, Build, Platform uint32
		ServiceName                         [128]uint16
		ServiceMajor, ServiceMinor, Suite   uint16
		Product, Reserved                   byte
	}
	info.Size = uint32(unsafe.Sizeof(info))
	status, _, _ := syscall.NewLazyDLL("ntdll.dll").NewProc("RtlGetVersion").Call(uintptr(unsafe.Pointer(&info)))
	if status == 0 && (info.Major < 10 || (info.Product == 1 && info.Build < 15063) || (info.Product != 1 && info.Build < 14393)) {
		return "Update to Windows 10 build 15063 or newer, or Windows Server 2016 or newer, before purchasing."
	}
	return ""
}
