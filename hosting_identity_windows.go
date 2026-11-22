package main

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

func systemInstallationID() (string, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Cryptography`, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err == nil {
		defer k.Close()
		if id, _, err := k.GetStringValue("MachineGuid"); err == nil {
			return id, nil
		}
	}
	return "", errors.New("Premium could not read this Windows installation's ID.")
}
