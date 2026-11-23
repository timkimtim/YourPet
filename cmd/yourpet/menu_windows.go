package main

import (
	"fmt"
	"time"
	"unsafe"
)

const (
	menuDisabled  = 0x0003
	menuChecked   = 0x0008
	menuPopup     = 0x0010
	menuSeparator = 0x0800
)

func menuItem(menu uintptr, id int, label string, checked, enabled bool) {
	var flags uintptr
	if checked {
		flags |= menuChecked
	}
	if !enabled {
		flags |= menuDisabled
	}
	appendMenu.Call(menu, flags, uintptr(id), uintptr(unsafe.Pointer(utf16(label))))
}

func submenu(parent uintptr, label string, enabled bool) uintptr {
	child, _, _ := createPopupMenu.Call()
	if child == 0 {
		return 0
	}
	flags := uintptr(menuPopup)
	if !enabled {
		flags |= menuDisabled
	}
	ok, _, _ := appendMenu.Call(parent, flags, child, uintptr(unsafe.Pointer(utf16(label))))
	if ok == 0 {
		destroyMenu.Call(child)
		return 0
	}
	return child
}

func (a *app) openMenu() {
	if a.menuOpen || a.dialogOpen || a.player == nil {
		return
	}
	pos, ok := cursorPosition()
	if !ok {
		return
	}
	menu, _, _ := createPopupMenu.Call()
	if menu == 0 {
		return
	}
	defer destroyMenu.Call(menu)
	pet := a.player.Pet
	menuItem(menu, 0, pet.Name, false, false)
	appendMenu.Call(menu, menuSeparator, 0, 0)
	menuItem(menu, commandLoad, tr("Load pet…", "Загрузить питомца…"), false, true)
	if a.imported {
		menuItem(menu, commandDefault, tr("Restore default pet", "Вернуть питомца по умолчанию"), false, true)
	}
	menuItem(menu, commandNextIdle, tr("Change idle pose", "Сменить позу"), false, len(pet.Idle) > 1)
	if list := submenu(menu, tr("Animations", "Анимации"), true); list != 0 {
		for index, clip := range pet.Animations {
			menuItem(list, commandAnimation+index, clip.Label, false, true)
		}
	}
	appendMenu.Call(menu, menuSeparator, 0, 0)
	menuItem(menu, commandAutoWalk, tr("Auto-walk", "Автопрогулки"), a.settings.AutoWalk && pet.Walk != nil, pet.Walk != nil)
	if list := submenu(menu, tr("Size", "Размер"), true); list != 0 {
		for index, size := range sizes {
			menuItem(list, commandSize+index, fmt.Sprintf("%d%%", size), a.settings.Scale == size, true)
		}
	}
	if list := submenu(menu, tr("Gaze radius", "Радиус взгляда"), pet.Look != nil); list != 0 {
		for index, radius := range radii {
			label := fmt.Sprintf(tr("%d pixels", "%d пикселей"), radius)
			if radius == 0 {
				label = tr("Off", "Выключен")
			}
			menuItem(list, commandRadius+index, label, a.settings.Radius == radius, true)
		}
	}
	menuItem(menu, commandTopmost, tr("Always on top", "Поверх окон"), a.settings.Topmost, true)
	menuItem(menu, commandHideFullscreen, tr("Hide in fullscreen", "Скрывать в полноэкранном режиме"), a.settings.HideFullscreen, true)
	menuItem(menu, commandClickThrough, tr("Click-through", "Пропускать клики"), a.settings.ClickThrough, a.trayAdded)
	menuItem(menu, commandStartup, tr("Start with Windows", "Запускать при входе в Windows"), launchesAtLogin(), true)
	appendMenu.Call(menu, menuSeparator, 0, 0)
	visibility := tr("Hide pet", "Спрятать питомца")
	if a.hidden {
		visibility = tr("Show pet", "Показать питомца")
	}
	menuItem(menu, commandVisibility, visibility, false, a.trayAdded)
	menuItem(menu, commandReposition, tr("Move to screen corner", "Вернуть в угол экрана"), false, true)
	menuItem(menu, commandQuit, tr("Exit", "Выход"), false, true)
	a.menuOpen = true
	setForegroundWindow.Call(a.window)
	selected, _, _ := trackPopupMenu.Call(menu, 0x0100|0x0080|0x0002, uintptr(pos.X), uintptr(pos.Y), a.window, 0)
	postMessage.Call(a.window, 0, 0, 0)
	a.menuOpen = false
	if selected != 0 {
		a.perform(int(selected))
	}
}

func (a *app) perform(command int) {
	now := time.Now()
	switch {
	case command >= commandSize && command < commandSize+len(sizes):
		area := a.monitor()
		oldWidth, oldHeight := a.size()
		a.settings.Scale = sizes[command-commandSize]
		width, height := a.size()
		a.x += float64(oldWidth-width) / 2
		a.y += float64(oldHeight - height)
		a.clamp(area, false)
		a.saveSettings()
	case command >= commandRadius && command < commandRadius+len(radii):
		a.settings.Radius = radii[command-commandRadius]
		a.saveSettings()
	case command >= commandAnimation && command < commandAnimation+len(a.player.Pet.Animations):
		a.player.Play(a.player.Pet.Animations[command-commandAnimation].ID, now)
		a.setVisible(true)
	default:
		switch command {
		case commandLoad:
			if a.importPackage() {
				a.setVisible(true)
			}
		case commandDefault:
			a.restoreDefault()
		case commandNextIdle:
			a.player.NextIdle(now)
		case commandAutoWalk:
			a.settings.AutoWalk = !a.settings.AutoWalk
			a.saveSettings()
		case commandTopmost:
			a.settings.Topmost = !a.settings.Topmost
			a.refreshStyle()
			a.saveSettings()
		case commandHideFullscreen:
			a.settings.HideFullscreen = !a.settings.HideFullscreen
			a.fullscreenCheckAt = time.Time{}
			a.saveSettings()
		case commandClickThrough:
			a.settings.ClickThrough = !a.settings.ClickThrough
			a.refreshStyle()
			a.saveSettings()
		case commandStartup:
			if err := setLaunchAtLogin(!launchesAtLogin(), a.executable); err != nil {
				a.error(tr("Could not change Windows startup", "Не удалось изменить автозагрузку"), err)
			}
		case commandVisibility:
			a.setVisible(a.hidden)
		case commandReposition:
			pos, _ := cursorPosition()
			a.placeInCorner(workArea(pos))
			a.saveSettings()
			a.setVisible(true)
		case commandQuit:
			destroyWindow.Call(a.window)
			return
		}
	}
	a.tick()
}
