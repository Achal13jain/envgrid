//go:build !windows

package main

import (
	"errors"
	"os"
	"runtime"

	"golang.org/x/sys/unix"
)

// errStoreUnsupported means this operating system has no store envgrid can use.
var errStoreUnsupported = errors.New("no supported secure key store on this system")

func platformSecretStore() (secretStore, error) {
	switch runtime.GOOS {
	case "darwin":
		return keychainStore{}, nil
	case "linux":
		return secretToolStore{}, nil
	}
	return nil, errStoreUnsupported
}

// lockFile takes an exclusive lock on f without waiting. The system drops it
// when the file is closed or the process ends.
func lockFile(f *os.File) error {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) {
		return errLockBusy
	}
	return err
}
