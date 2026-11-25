package main

import (
	"bufio"
	"bytes"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	utf16encoding "unicode/utf16"
	"unsafe"
)

//go:embed uninstall.ps1
var uninstallScript string

var (
	taskDialogIndirect = syscall.NewLazyDLL("comctl32.dll").NewProc("TaskDialogIndirect")
	getSystemDirectory = kernel32.NewProc("GetSystemDirectoryW")
)

func (a *app) uninstall() {
	if a.dialogOpen || a.updates.pending != nil {
		return
	}
	a.dialogOpen = true
	defer func() { a.dialogOpen = false }()
	confirmed, removeData := confirmUninstall(a.window)
	if !confirmed {
		return
	}
	configDir := ""
	if removeData {
		configDir = a.configDir
	}
	cmd, err := uninstallCommand(a.executable, configDir, startupKey, startupName)
	if err == nil {
		err = startUninstall(cmd)
	}
	if err != nil {
		a.error(tr("Could not start uninstalling YourPet", "Не удалось запустить удаление YourPet"), err)
		return
	}
	destroyWindow.Call(a.window)
}

func confirmUninstall(window uintptr) (confirmed, removeData bool) {
	heading := tr("Uninstall YourPet?", "Удалить YourPet?")
	detail := tr("YourPet will close, remove its executable, and turn off its Windows autostart.",
		"YourPet закроется, удалит свой файл и отключит автозапуск в Windows.")
	checkbox := tr("Also delete saved settings and the imported pet", "Также удалить настройки и загруженного питомца")
	if err := taskDialogIndirect.Find(); err == nil {
		const removeID, cancelID = 100, 2
		const commonCancelButton = 0x8
		title, mainText, body, verification, remove := utf16("YourPet"), utf16(heading), utf16(detail),
			utf16(checkbox), utf16(tr("Uninstall", "Удалить"))
		// The Windows SDK packs TASKDIALOGCONFIG and TASKDIALOG_BUTTON on
		// one-byte boundaries. Both supported Windows targets have 64-bit pointers.
		var config [160]byte
		var button [12]byte
		var pinned runtime.Pinner
		defer pinned.Unpin()
		pinned.Pin(&button)
		for _, label := range []*uint16{title, mainText, body, verification, remove} {
			pinned.Pin(label)
		}
		binary.LittleEndian.PutUint32(button[:], removeID)
		binary.LittleEndian.PutUint64(button[4:], uint64(uintptr(unsafe.Pointer(remove))))
		binary.LittleEndian.PutUint32(config[:], uint32(len(config)))
		binary.LittleEndian.PutUint64(config[4:], uint64(window))
		binary.LittleEndian.PutUint32(config[24:], commonCancelButton)
		binary.LittleEndian.PutUint64(config[28:], uint64(uintptr(unsafe.Pointer(title))))
		binary.LittleEndian.PutUint64(config[44:], uint64(uintptr(unsafe.Pointer(mainText))))
		binary.LittleEndian.PutUint64(config[52:], uint64(uintptr(unsafe.Pointer(body))))
		binary.LittleEndian.PutUint32(config[60:], 1)
		binary.LittleEndian.PutUint64(config[64:], uint64(uintptr(unsafe.Pointer(&button))))
		binary.LittleEndian.PutUint32(config[72:], cancelID)
		binary.LittleEndian.PutUint64(config[92:], uint64(uintptr(unsafe.Pointer(verification))))
		var selected, checked int32
		status, _, _ := taskDialogIndirect.Call(uintptr(unsafe.Pointer(&config)),
			uintptr(unsafe.Pointer(&selected)), 0, uintptr(unsafe.Pointer(&checked)))
		if int32(status) >= 0 {
			return selected == removeID, checked != 0
		}
	}

	const yesNoDefaultNo = 0x0004 | 0x0020 | 0x0100
	const idYes = 6
	if result, _, _ := messageBox.Call(window, uintptr(unsafe.Pointer(utf16(heading+"\n\n"+detail))),
		uintptr(unsafe.Pointer(utf16("YourPet"))), yesNoDefaultNo); result != idYes {
		return false, false
	}
	prompt := tr("Also delete saved settings and the imported pet?", "Удалить и настройки с загруженным питомцем?")
	result, _, _ := messageBox.Call(window, uintptr(unsafe.Pointer(utf16(prompt))),
		uintptr(unsafe.Pointer(utf16("YourPet"))), yesNoDefaultNo)
	return true, result == idYes
}

func uninstallCommand(target, configDir, registryKey, registryName string) (*exec.Cmd, error) {
	var systemDir [260]uint16
	length, _, err := getSystemDirectory.Call(uintptr(unsafe.Pointer(&systemDir)), uintptr(len(systemDir)))
	if length == 0 {
		return nil, err
	}
	if length >= uintptr(len(systemDir)) {
		return nil, errors.New("the Windows system directory path is too long")
	}
	powerShell := filepath.Join(syscall.UTF16ToString(systemDir[:length]),
		"WindowsPowerShell", "v1.0", "powershell.exe")
	content := utf16encoding.Encode([]rune(uninstallScript))
	encoded := make([]byte, len(content)*2)
	for i, unit := range content {
		binary.LittleEndian.PutUint16(encoded[i*2:], unit)
	}
	cmd := exec.Command(powerShell, "-NoLogo", "-NoProfile", "-NonInteractive",
		"-EncodedCommand", base64.StdEncoding.EncodeToString(encoded))
	cmd.Dir = filepath.Dir(powerShell)
	const createNoWindow = 0x08000000
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
	cmd.Env = append(os.Environ(),
		"YOURPET_UNINSTALL_PARENT="+strconv.Itoa(os.Getpid()),
		"YOURPET_UNINSTALL_EXE="+target,
		"YOURPET_UNINSTALL_DATA="+configDir,
		"YOURPET_UNINSTALL_STARTUP_KEY="+registryKey,
		"YOURPET_UNINSTALL_STARTUP_NAME="+registryName,
		"YOURPET_UNINSTALL_ERROR="+tr("YourPet could not finish uninstalling. If the application is still present, open it and try again.",
			"Не удалось завершить удаление YourPet. Если приложение осталось, откройте его и попробуйте ещё раз."),
	)
	return cmd, nil
}

// Wait for the Windows helper before closing the app. EOF means cancel, so a
// failed handshake cannot change settings, the executable, or autostart.
func startUninstall(cmd *exec.Cmd) error {
	control, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	defer control.Close()
	output, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var diagnostics bytes.Buffer
	cmd.Stderr = &diagnostics
	if err := cmd.Start(); err != nil {
		return err
	}
	stop := func() {
		control.Close()
		cmd.Process.Kill()
		cmd.Wait()
	}
	ready := make(chan bool, 1)
	go func() {
		line, err := bufio.NewReader(output).ReadString('\n')
		ready <- err == nil && strings.TrimPrefix(strings.TrimSpace(line), "\uFEFF") == "ready"
	}()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case ok := <-ready:
		if !ok {
			stop()
			if detail := strings.TrimSpace(diagnostics.String()); detail != "" {
				return fmt.Errorf("Windows PowerShell could not start the uninstaller: %s", detail)
			}
			return errors.New("Windows PowerShell did not start the uninstaller")
		}
	case <-timer.C:
		stop()
		return errors.New("timed out waiting for the Windows uninstaller")
	}
	if _, err := io.WriteString(control, "remove\n"); err != nil {
		stop()
		return err
	}
	go cmd.Wait()
	return nil
}
