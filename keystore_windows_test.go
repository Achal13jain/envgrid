package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestDPAPIKeepsTheKeyEncrypted(t *testing.T) {
	s := dpapiStore{path: filepath.Join(t.TempDir(), "master-key.dpapi")}
	key := []byte("bC7xQ0lQ3oV1V6m8xZJp9b6yYQ3m1x8k7n6s5d4f3g2=")
	if err := s.Set(key); err != nil {
		t.Fatal(err)
	}
	blob, err := os.ReadFile(s.path)
	if err != nil || bytes.Contains(blob, key) {
		t.Fatalf("the file must not hold the key in the clear: %v", err)
	}
	got, err := s.Get()
	if err != nil || !bytes.Equal(got, key) {
		t.Fatalf("round trip: %q %v", got, err)
	}
	if err := s.Delete([]byte("not-this-key")); err != nil || !fileExists(s.path) {
		t.Fatalf("delete with another key must leave the stored key: %v", err)
	}
	if err := s.Delete(key); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(); !errors.Is(err, errKeyNotStored) || fileExists(s.path) {
		t.Fatalf("after delete the key must be gone and reported as not stored: %v", err)
	}
}

func TestSamePathKnowsAShortName(t *testing.T) {
	dir := t.TempDir()
	long, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]uint16, 1024)
	n, err := windows.GetShortPathName(long, &buf[0], uint32(len(buf)))
	short := windows.UTF16ToString(buf[:n])
	if err != nil || short == dir {
		t.Skipf("no short name for %s: %v", dir, err)
	}
	if !samePath(short, dir) {
		t.Fatalf("%s and %s are the same folder", short, dir)
	}
}

func TestSetupLivesInTheLocalProfile(t *testing.T) {
	dir, err := setupDir()
	want := filepath.Join(os.Getenv("LOCALAPPDATA"), "envgrid")
	if err != nil || !strings.EqualFold(dir, want) {
		t.Fatalf("settings folder %q, want %q, the part of the profile that does not roam: %v", dir, want, err)
	}
}

func TestSamePathKnowsAShortNameOfAMissingFolder(t *testing.T) {
	dir := t.TempDir()
	long, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]uint16, 1024)
	n, err := windows.GetShortPathName(long, &buf[0], uint32(len(buf)))
	short := windows.UTF16ToString(buf[:n])
	if err != nil || short == dir {
		t.Skipf("no short name for %s: %v", dir, err)
	}
	if !samePath(filepath.Join(short, "missing"), filepath.Join(dir, "missing")) {
		t.Fatalf("%s and %s are the same missing folder", short, dir)
	}
}
