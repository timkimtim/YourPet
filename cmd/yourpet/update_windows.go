package main

import (
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	yourpet "github.com/timkimtim/YourPet"
	"github.com/timkimtim/YourPet/internal/update"
)

const updateCheckInterval = 24 * time.Hour

type updateState struct {
	available *update.Release
	nextCheck time.Time
	pending   chan updateResult
	manual    bool
	download  bool
}

type updateResult struct {
	release    *update.Release
	executable []byte
	err        error
}

func (a *app) updateMenuLabel() string {
	switch {
	case a.updates.download:
		return tr("Downloading update…", "Скачивание обновления…")
	case a.updates.pending != nil:
		return tr("Checking for updates…", "Проверка обновлений…")
	case a.updates.available != nil:
		return fmt.Sprintf(tr("Install update %s…", "Установить обновление %s…"), a.updates.available.Tag)
	default:
		return tr("Check for updates…", "Проверить обновления…")
	}
}

func (a *app) checkUpdates(manual bool) {
	if a.updates.pending != nil {
		return
	}
	results := make(chan updateResult, 1)
	a.updates.pending, a.updates.manual = results, manual
	a.updates.nextCheck = time.Now().Add(updateCheckInterval)
	go func() {
		release, err := update.Check(yourpet.Version(), runtime.GOARCH)
		results <- updateResult{release: release, err: err}
	}()
}

func (a *app) offerUpdate() {
	if a.updates.pending != nil || a.dialogOpen || a.menuOpen {
		return
	}
	release := a.updates.available
	if release == nil {
		a.checkUpdates(true)
		return
	}
	prompt := fmt.Sprintf(tr("Download and install YourPet %s? The app will restart after downloading.",
		"Скачать и установить YourPet %s? После скачивания приложение перезапустится."), release.Tag)
	a.dialogOpen = true
	const yesNoQuestionDefaultNo = 0x0004 | 0x0020 | 0x0100
	result, _, _ := messageBox.Call(a.window, uintptr(unsafe.Pointer(utf16(prompt))),
		uintptr(unsafe.Pointer(utf16("YourPet"))), yesNoQuestionDefaultNo)
	a.dialogOpen = false
	if result != 6 { // IDYES
		return
	}
	results := make(chan updateResult, 1)
	a.updates.pending, a.updates.download = results, true
	go func() {
		executable, err := release.Download()
		results <- updateResult{executable: executable, err: err}
	}()
}

func (a *app) pollUpdates() {
	if a.dialogOpen || a.menuOpen || a.dragDown {
		return
	}
	if pending := a.updates.pending; pending != nil {
		select {
		case result := <-pending:
			manual, downloading := a.updates.manual, a.updates.download
			a.updates.pending, a.updates.manual, a.updates.download = nil, false, false
			if downloading {
				a.finishDownload(result)
			} else {
				a.finishUpdateCheck(result, manual)
			}
		default:
		}
		return
	}
	if !a.updates.nextCheck.IsZero() && !time.Now().Before(a.updates.nextCheck) {
		a.checkUpdates(false)
	}
}

func (a *app) finishUpdateCheck(result updateResult, manual bool) {
	if result.err != nil {
		if manual {
			a.updateMessage(tr("Could not check for updates", "Не удалось проверить обновления")+"\n\n"+result.err.Error(), 0x10)
		}
		return
	}
	previous := a.updates.available
	a.updates.available = result.release
	switch {
	case result.release == nil && manual:
		a.updateMessage(tr("YourPet is up to date.", "Установлена актуальная версия YourPet."), 0x40)
	case result.release != nil && manual:
		a.offerUpdate()
	case result.release != nil && (previous == nil || result.release.Tag != previous.Tag):
		a.notifyUpdate(result.release.Tag)
	}
}

func (a *app) finishDownload(result updateResult) {
	err := result.err
	if err == nil {
		err = startUpdate(a.executable, result.executable)
	}
	if err != nil {
		a.updateMessage(tr("Could not install the update", "Не удалось установить обновление")+"\n\n"+err.Error(), 0x10)
		return
	}
	destroyWindow.Call(a.window)
}

func (a *app) updateMessage(text string, flag uintptr) {
	a.dialogOpen = true
	showMessage(a.window, text, flag)
	a.dialogOpen = false
}

func (a *app) notifyUpdate(tag string) {
	if !a.trayAdded {
		return
	}
	data := iconData{
		Size: uint32(unsafe.Sizeof(iconData{})), Window: a.window, ID: 1,
		Flags: 0x10, InfoFlags: 0x81, // NIF_INFO, NIIF_INFO | NIIF_RESPECT_QUIET_TIME
	}
	copy(data.InfoTitle[:], syscall.StringToUTF16("YourPet "+tag))
	copy(data.Info[:], syscall.StringToUTF16(tr("An update is available. You can install it from the YourPet menu.",
		"Доступно обновление. Установить его можно из меню YourPet.")))
	notifyIcon.Call(1, uintptr(unsafe.Pointer(&data))) // NIM_MODIFY
}
