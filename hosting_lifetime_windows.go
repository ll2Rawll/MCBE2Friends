package main

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows closes the launcher's last job handle if it exits unexpectedly.
// Keep the job private to Bedrock so a browser opened by the launcher is independent.
func bindBedrockLifetime(process *os.Process) (func(), error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	release := func() { _ = windows.CloseHandle(job) }
	var limits windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	_, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits)))
	if err == nil {
		var assignErr error
		err = process.WithHandle(func(handle uintptr) {
			assignErr = windows.AssignProcessToJobObject(job, windows.Handle(handle))
		})
		if err == nil {
			err = assignErr
		}
	}
	if err != nil {
		release()
		return nil, err
	}
	return release, nil
}
