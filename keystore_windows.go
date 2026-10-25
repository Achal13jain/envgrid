package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// dpapiStore keeps the key encrypted with Windows data protection (DPAPI) for
// the signed-in user: only the same Windows account on the same computer can
// decrypt it, and DPAPI adds an integrity check. The encrypted file lives in
// the user's settings folder, not beside the database.
type dpapiStore struct{ path string }

func platformSecretStore() (secretStore, error) {
	dir, err := setupDir()
	if err != nil {
		return nil, err
	}
	return dpapiStore{path: filepath.Join(dir, "master-key.dpapi")}, nil
}

// dpapiEntropy ties the protected data to envgrid, so another program's DPAPI
// data cannot be swapped in for it.
var dpapiEntropy = []byte("envgrid master key v1")

func (dpapiStore) Name() string { return "Windows data protection (DPAPI) for your Windows account" }

func (s dpapiStore) Get() ([]byte, error) {
	blob, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, errKeyNotStored
	}
	if err != nil {
		return nil, err
	}
	return dpapi(blob, false)
}

func (s dpapiStore) Set(key []byte) error {
	blob, err := dpapi(key, true)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, blob, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s dpapiStore) Delete(key []byte) error {
	return deleteIfStored(s, key, func() error {
		err := os.Remove(s.path)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	})
}

// lockFile takes an exclusive lock on f without waiting. Windows drops it when
// the file is closed or the process ends.
func lockFile(f *os.File) error {
	err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, new(windows.Overlapped))
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return errLockBusy
	}
	return err
}

func dpapi(in []byte, protect bool) ([]byte, error) {
	if len(in) == 0 {
		return nil, errors.New("nothing to protect")
	}
	data := windows.DataBlob{Size: uint32(len(in)), Data: &in[0]}
	entropy := windows.DataBlob{Size: uint32(len(dpapiEntropy)), Data: &dpapiEntropy[0]}
	var out windows.DataBlob
	var err error
	if protect {
		err = windows.CryptProtectData(&data, nil, &entropy, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	} else {
		err = windows.CryptUnprotectData(&data, nil, &entropy, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	}
	if err != nil {
		return nil, err
	}
	// DPAPI returns memory the caller frees with LocalFree; copy it out first.
	defer func() { _, _ = windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data))) }() //nolint:gosec // G103: required by the DPAPI calling convention
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil                  //nolint:gosec // G103: reads the DPAPI output buffer
}
