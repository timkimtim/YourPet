package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const testUninstallArg = "--test-uninstall"

func runUninstallTestProcess(configDir, key string) error {
	target, err := os.Executable()
	if err != nil {
		return err
	}
	cmd, err := uninstallCommand(target, configDir, key, startupName)
	if err != nil {
		return err
	}
	cmd.Env = append(cmd.Env, "YOURPET_UNINSTALL_ERROR=")
	return startUninstall(cmd)
}

func TestWindowsUninstall(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name       string
		removeData bool
	}{
		{"keep settings", false},
		{"delete settings", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			folder := t.TempDir()
			target := filepath.Join(folder, "Персик [portable] & pet.exe")
			if err := copyForUpdate(self, target); err != nil {
				t.Fatal(err)
			}
			config := filepath.Join(folder, "Сохранённые данные [Persik]")
			if err := os.Mkdir(config, 0700); err != nil {
				t.Fatal(err)
			}
			files := []string{filepath.Join(config, "settings.json"), filepath.Join(config, "current.pet"),
				filepath.Join(config, "my notes.txt"), filepath.Join(folder, "README.md")}
			for _, path := range files {
				if err := os.WriteFile(path, []byte("uninstall test"), 0600); err != nil {
					t.Fatal(err)
				}
			}

			key := fmt.Sprintf(`Software\YourPetUninstallTest-%d-%d`, os.Getpid(), time.Now().UnixNano())
			fullKey := `HKCU\` + key
			t.Cleanup(func() { exec.Command("reg.exe", "delete", fullKey, "/f").Run() })
			for _, value := range []string{startupName, "OtherApp"} {
				cmd := exec.Command("reg.exe", "add", fullKey, "/v", value, "/d", target, "/f")
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("create test startup entry: %v\n%s", err, out)
				}
			}
			option := ""
			if test.removeData {
				option = config
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, target, testUninstallArg, option, key)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("launch uninstaller: %v\n%s", err, out)
			}

			removed := []string{target}
			kept := files
			if test.removeData {
				removed = append(removed, files[:2]...)
				kept = files[2:]
			}
			deadline := time.Now().Add(12 * time.Second)
			for _, file := range removed {
				for {
					if _, err := os.Stat(file); errors.Is(err, os.ErrNotExist) {
						break
					} else if err != nil || time.Now().After(deadline) {
						t.Fatalf("uninstaller did not delete %s: %v", file, err)
					}
					time.Sleep(50 * time.Millisecond)
				}
			}
			for _, file := range kept {
				if data, err := os.ReadFile(file); err != nil || string(data) != "uninstall test" {
					t.Fatalf("uninstaller modified %s: %v", file, err)
				}
			}
			if out, err := exec.Command("reg.exe", "query", fullKey, "/v", "OtherApp").CombinedOutput(); err != nil {
				t.Fatalf("uninstaller removed the unrelated startup entry: %v\n%s", err, out)
			}
			if err := exec.Command("reg.exe", "query", fullKey, "/v", startupName).Run(); err == nil {
				t.Fatal("uninstaller left the YourPet startup entry")
			}
		})
	}
}
