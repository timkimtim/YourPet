package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	updateStagePrefix = ".yourpet-update-"
	updateHelper      = "apply.exe"
	updateCandidate   = "new.exe"
	updateBackup      = "previous.exe"
	applyUpdateArg    = "--apply-update"
	updatedArg        = "--update-ready"
	recoveredArg      = "--update-recovered"
)

var (
	createUpdateEvent = kernel32.NewProc("CreateEventW")
	setUpdateEvent    = kernel32.NewProc("SetEvent")
)

func startUpdate(target string, data []byte) error {
	if !filepath.IsAbs(target) || !strings.EqualFold(filepath.Ext(target), ".exe") {
		return errors.New("invalid path to the installed YourPet executable")
	}
	stage, err := os.MkdirTemp(filepath.Dir(target), updateStagePrefix)
	if err != nil {
		return err
	}
	started := false
	defer func() {
		if !started {
			os.RemoveAll(stage)
		}
	}()
	if err := os.WriteFile(filepath.Join(stage, updateCandidate), data, 0600); err != nil {
		return err
	}
	helper := filepath.Join(stage, updateHelper)
	if err := copyForUpdate(target, helper); err != nil {
		return err
	}
	self, err := syscall.GetCurrentProcess()
	if err != nil {
		return err
	}
	var parent syscall.Handle
	const synchronize = 0x00100000
	if err := syscall.DuplicateHandle(self, self, self, &parent, synchronize, true, 0); err != nil {
		return err
	}
	defer syscall.CloseHandle(parent)
	cmd := updateCommand(helper, parent, applyUpdateArg, target, handleArg(parent))
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	started = true
	return nil
}

func runUpdateHelper(target, parentArg string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	stage := filepath.Dir(self)
	if !validUpdateStage(stage, target) || !strings.EqualFold(filepath.Base(self), updateHelper) {
		return errors.New("invalid update helper location")
	}
	parent, err := parseUpdateHandle(parentArg)
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(parent)
	// Wait for both the executable image and the single-instance mutex to be released.
	if err := waitForUpdateHandle(parent, 30_000); err != nil {
		return fmt.Errorf("waiting for YourPet to exit: %w", err)
	}

	backup := filepath.Join(stage, updateBackup)
	if err := copyForUpdate(target, backup); err != nil {
		return restartPrevious(target, stage, err)
	}
	if err := replaceUpdateFile(filepath.Join(stage, updateCandidate), target); err != nil {
		return restartPrevious(target, stage, err)
	}
	if err := launchUpdated(target, stage); err != nil {
		if restoreErr := replaceUpdateFile(backup, target); restoreErr != nil {
			return fmt.Errorf("%w\n%s\n%s: %w", err,
				tr("The previous version is saved at", "Предыдущая версия сохранена по пути"), backup, restoreErr)
		}
		return restartPrevious(target, stage, err)
	}
	return nil
}

func launchUpdated(target, stage string) error {
	security := syscall.SecurityAttributes{InheritHandle: 1}
	security.Length = uint32(unsafe.Sizeof(security))
	value, _, err := createUpdateEvent.Call(uintptr(unsafe.Pointer(&security)), 0, 0, 0)
	if value == 0 {
		return err
	}
	ready := syscall.Handle(value)
	defer syscall.CloseHandle(ready)
	cmd := updateCommand(target, ready, updatedArg, stage, handleArg(ready))
	if err := cmd.Start(); err != nil {
		return err
	}
	if err := waitForUpdateHandle(ready, 20_000); err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return fmt.Errorf("the new version did not start: %w", err)
	}
	go cmd.Wait()
	return nil
}

func restartPrevious(target, stage string, cause error) error {
	cmd := updateCommand(target, 0, recoveredArg, stage, cause.Error())
	if err := cmd.Start(); err != nil {
		return errors.Join(cause, err)
	}
	go cmd.Wait()
	return nil
}

func completeUpdateStartup(target string, args []string) string {
	if len(args) != 3 || !validUpdateStage(args[1], target) {
		return ""
	}
	switch args[0] {
	case updatedArg:
		ready, err := parseUpdateHandle(args[2])
		if err != nil {
			return ""
		}
		ok, _, _ := setUpdateEvent.Call(uintptr(ready))
		syscall.CloseHandle(ready)
		if ok == 0 {
			return ""
		}
	case recoveredArg:
	default:
		return ""
	}
	go cleanUpdateStage(args[1])
	if args[0] == recoveredArg {
		return args[2]
	}
	return ""
}

func cleanUpdateStage(stage string) {
	// Windows keeps the updater image open until the helper exits.
	for attempt := 0; attempt < 25; attempt++ {
		for _, name := range []string{updateCandidate, updateBackup, updateHelper} {
			os.Remove(filepath.Join(stage, name))
		}
		if err := os.Remove(stage); err == nil || errors.Is(err, os.ErrNotExist) {
			return
		}
		time.Sleep(400 * time.Millisecond)
	}
}

func validUpdateStage(stage, target string) bool {
	return filepath.IsAbs(stage) && filepath.IsAbs(target) && strings.EqualFold(filepath.Ext(target), ".exe") &&
		strings.EqualFold(filepath.Dir(filepath.Clean(stage)), filepath.Dir(filepath.Clean(target))) &&
		strings.HasPrefix(filepath.Base(stage), updateStagePrefix)
}

func updateCommand(path string, inherit syscall.Handle, args ...string) *exec.Cmd {
	cmd := exec.Command(path, args...)
	cmd.Dir = filepath.Dir(path)
	if inherit != 0 {
		cmd.SysProcAttr = &syscall.SysProcAttr{AdditionalInheritedHandles: []syscall.Handle{inherit}}
	}
	return cmd
}

func handleArg(handle syscall.Handle) string {
	return strconv.FormatUint(uint64(handle), 10)
}

func parseUpdateHandle(arg string) (syscall.Handle, error) {
	value, err := strconv.ParseUint(arg, 10, strconv.IntSize)
	if err != nil || value == 0 || value == uint64(^uintptr(0)) {
		return 0, errors.New("invalid update handle")
	}
	return syscall.Handle(value), nil
}

func waitForUpdateHandle(handle syscall.Handle, millis uint32) error {
	event, err := syscall.WaitForSingleObject(handle, millis)
	if event == syscall.WAIT_OBJECT_0 {
		return nil
	}
	if event == syscall.WAIT_TIMEOUT {
		return errors.New("timed out")
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("unexpected wait result %#x", event)
}

func copyForUpdate(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = io.Copy(output, input)
	return errors.Join(err, output.Close())
}

func replaceUpdateFile(from, to string) error {
	const moveReplaceAndWriteThrough = 0x9
	var err error
	for attempt := 0; attempt < 12; attempt++ {
		ok, _, cause := moveFile.Call(uintptr(unsafe.Pointer(utf16(from))),
			uintptr(unsafe.Pointer(utf16(to))), moveReplaceAndWriteThrough)
		if ok != 0 {
			return nil
		}
		err = cause
		if cause != syscall.Errno(5) && cause != syscall.Errno(32) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return &os.LinkError{Op: "replace executable", Old: from, New: to, Err: err}
}
