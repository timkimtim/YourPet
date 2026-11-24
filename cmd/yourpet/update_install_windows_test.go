package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const testStartUpdateArg = "--test-start-update"

func TestMain(m *testing.M) {
	var err error
	switch {
	case len(os.Args) == 3 && os.Args[1] == testStartUpdateArg:
		var data []byte
		if data, err = os.ReadFile(os.Args[2]); err == nil {
			var target string
			if target, err = os.Executable(); err == nil {
				err = startUpdate(target, data)
			}
		}
	case len(os.Args) == 4 && os.Args[1] == applyUpdateArg:
		err = runUpdateHelper(os.Args[2], os.Args[3])
	case len(os.Args) == 4 && (os.Args[1] == updatedArg || os.Args[1] == recoveredArg):
		var target string
		if target, err = os.Executable(); err == nil {
			completeUpdateStartup(target, os.Args[1:])
			err = waitForRemovedStage(filepath.Dir(target))
			if err == nil {
				err = os.WriteFile(filepath.Join(filepath.Dir(target), "restart.txt"), []byte(os.Args[1]), 0600)
			}
		}
	default:
		os.Exit(m.Run())
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func TestWindowsUpdateAndRecovery(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	replacement := append(bytes.Clone(original), []byte("YourPet test version")...)
	for _, test := range []struct {
		name, expectedRestart string
		candidate, expected   []byte
	}{
		{"install", updatedArg, replacement, replacement},
		{"rollback", recoveredArg, []byte("invalid executable"), original},
	} {
		t.Run(test.name, func(t *testing.T) {
			folder := filepath.Join(t.TempDir(), "Persik — portable")
			if err := os.Mkdir(folder, 0700); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(folder, "YourPet.exe")
			candidate := filepath.Join(folder, "candidate.exe")
			if err := os.WriteFile(target, original, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(candidate, test.candidate, 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, target, testStartUpdateArg, candidate)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("stage update: %v\n%s", err, out)
			}
			marker := filepath.Join(folder, "restart.txt")
			for deadline := time.Now().Add(12 * time.Second); ; time.Sleep(50 * time.Millisecond) {
				state, err := os.ReadFile(marker)
				if err == nil && len(state) != 0 {
					if string(state) != test.expectedRestart {
						t.Fatalf("unexpected restart: %s", state)
					}
					break
				}
				if (err != nil && !errors.Is(err, os.ErrNotExist)) || time.Now().After(deadline) {
					t.Fatalf("app did not restart and clean up after update: %v", err)
				}
			}
			actual, err := os.ReadFile(target)
			if err != nil || !bytes.Equal(actual, test.expected) {
				t.Fatalf("unexpected installed binary after restart: %v", err)
			}
		})
	}
}

func waitForRemovedStage(folder string) error {
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		stages, err := filepath.Glob(filepath.Join(folder, updateStagePrefix+"*"))
		if err != nil || len(stages) == 0 {
			return err
		}
	}
	return errors.New("the update staging directory was not cleaned up")
}
