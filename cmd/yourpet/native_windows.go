package main

import (
	"errors"
	"fmt"
	"image"
	"math"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")
	comdlg32 = syscall.NewLazyDLL("comdlg32.dll")

	createWindow          = user32.NewProc("CreateWindowExW")
	defaultWindowProc     = user32.NewProc("DefWindowProcW")
	registerClass         = user32.NewProc("RegisterClassExW")
	getMessage            = user32.NewProc("GetMessageW")
	translateMessage      = user32.NewProc("TranslateMessage")
	dispatchMessage       = user32.NewProc("DispatchMessageW")
	destroyWindow         = user32.NewProc("DestroyWindow")
	postQuitMessage       = user32.NewProc("PostQuitMessage")
	postMessage           = user32.NewProc("PostMessageW")
	getCursor             = user32.NewProc("GetCursorPos")
	getMonitor            = user32.NewProc("MonitorFromPoint")
	getMonitorInfo        = user32.NewProc("GetMonitorInfoW")
	setTimer              = user32.NewProc("SetTimer")
	killTimer             = user32.NewProc("KillTimer")
	showWindow            = user32.NewProc("ShowWindow")
	setCapture            = user32.NewProc("SetCapture")
	releaseCapture        = user32.NewProc("ReleaseCapture")
	setWindowPos          = user32.NewProc("SetWindowPos")
	getWindowLong         = user32.NewProc("GetWindowLongPtrW")
	setWindowLong         = user32.NewProc("SetWindowLongPtrW")
	updateLayeredWindow   = user32.NewProc("UpdateLayeredWindow")
	getWindowDPI          = user32.NewProc("GetDpiForWindow")
	setDPIContext         = user32.NewProc("SetProcessDpiAwarenessContext")
	messageBox            = user32.NewProc("MessageBoxW")
	setForegroundWindow   = user32.NewProc("SetForegroundWindow")
	loadCursor            = user32.NewProc("LoadCursorW")
	loadIcon              = user32.NewProc("LoadIconW")
	registerWindowMessage = user32.NewProc("RegisterWindowMessageW")
	findWindow            = user32.NewProc("FindWindowW")

	createPopupMenu = user32.NewProc("CreatePopupMenu")
	appendMenu      = user32.NewProc("AppendMenuW")
	trackPopupMenu  = user32.NewProc("TrackPopupMenuEx")
	destroyMenu     = user32.NewProc("DestroyMenu")

	createDC     = gdi32.NewProc("CreateCompatibleDC")
	createDIB    = gdi32.NewProc("CreateDIBSection")
	selectObject = gdi32.NewProc("SelectObject")
	deleteDC     = gdi32.NewProc("DeleteDC")
	deleteObject = gdi32.NewProc("DeleteObject")

	createMutex     = kernel32.NewProc("CreateMutexW")
	closeHandle     = kernel32.NewProc("CloseHandle")
	getModuleHandle = kernel32.NewProc("GetModuleHandleW")
	moveFile        = kernel32.NewProc("MoveFileExW")
	notifyIcon      = shell32.NewProc("Shell_NotifyIconW")
	selectFile      = comdlg32.NewProc("GetOpenFileNameW")
	fileDialogError = comdlg32.NewProc("CommDlgExtendedError")
	regGet          = advapi32.NewProc("RegGetValueW")
	regSet          = advapi32.NewProc("RegSetKeyValueW")
	regDelete       = advapi32.NewProc("RegDeleteKeyValueW")
)

type point struct{ X, Y int32 }
type rectangle struct{ Left, Top, Right, Bottom int32 }

type windowClass struct {
	Size, Style                        uint32
	Proc                               uintptr
	ExtraClass, ExtraWindow            int32
	Instance, Icon, Cursor, Background uintptr
	Menu, Name                         *uint16
	SmallIcon                          uintptr
}

type message struct {
	Window         uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Point          point
	Private        uint32
}

type monitorInfo struct {
	Size          uint32
	Monitor, Work rectangle
	Flags         uint32
}

type iconData struct {
	Size                uint32
	Window              uintptr
	ID, Flags, Callback uint32
	Icon                uintptr
	Tip                 [128]uint16
	State, StateMask    uint32
	Info                [256]uint16
	Version             uint32
	InfoTitle           [64]uint16
	InfoFlags           uint32
	GUID                [16]byte
	BalloonIcon         uintptr
}

type openFileName struct {
	Size                         uint32
	Owner, Instance              uintptr
	Filter, CustomFilter         *uint16
	MaxCustomFilter, FilterIndex uint32
	File                         *uint16
	MaxFile                      uint32
	FileTitle                    *uint16
	MaxFileTitle                 uint32
	InitialDir, Title            *uint16
	Flags                        uint32
	FileOffset, ExtensionOffset  uint16
	DefaultExt                   *uint16
	CustomData, Hook             uintptr
	Template                     *uint16
	Reserved                     uintptr
	ReservedSize, FlagsEx        uint32
}

const (
	wmDestroy            = 0x0002
	wmClose              = 0x0010
	wmTimer              = 0x0113
	wmDPICHanged         = 0x02e0
	wmDisplayChange      = 0x007e
	wmLButtonDown        = 0x0201
	wmLButtonUp          = 0x0202
	wmLButtonDoubleClick = 0x0203
	wmMouseMove          = 0x0200
	wmRButtonUp          = 0x0205
	wmCaptureChanged     = 0x0215
	wmCancelMode         = 0x001f
	wmTray               = 0x8001
	wmShowPet            = 0x8002
	wsExLayered          = 0x00080000
	wsExToolWindow       = 0x00000080
	wsExTransparent      = 0x00000020
	wsExNoActivate       = 0x08000000
)

var getUILanguage = kernel32.NewProc("GetUserDefaultUILanguage")
var russianUI bool

func tr(english, russian string) string {
	if russianUI {
		return russian
	}
	return english
}

func utf16(value string) *uint16 {
	ptr, err := syscall.UTF16PtrFromString(value)
	if err != nil {
		return syscall.StringToUTF16Ptr("YourPet")
	}
	return ptr
}

func cursorPosition() (point, bool) {
	var pos point
	ok, _, _ := getCursor.Call(uintptr(unsafe.Pointer(&pos)))
	return pos, ok != 0
}

func workArea(at point) rectangle {
	return displayInfo(monitorAt(at)).Work
}

func monitorAt(at point) uintptr {
	packed := uintptr(uint64(uint32(at.X)) | uint64(uint32(at.Y))<<32)
	monitor, _, _ := getMonitor.Call(packed, 2) // MONITOR_DEFAULTTONEAREST
	return monitor
}

func displayInfo(monitor uintptr) monitorInfo {
	info := monitorInfo{Size: uint32(unsafe.Sizeof(monitorInfo{}))}
	if ok, _, _ := getMonitorInfo.Call(monitor, uintptr(unsafe.Pointer(&info))); ok == 0 {
		info.Work = rectangle{Right: 1024, Bottom: 768}
	}
	return info
}

func showMessage(hwnd uintptr, text string, flags uintptr) {
	messageBox.Call(hwnd, uintptr(unsafe.Pointer(utf16(text))), uintptr(unsafe.Pointer(utf16("YourPet"))), flags)
}

func choosePackage(hwnd uintptr) (string, error) {
	buffer := make([]uint16, 32768)
	filter := []uint16{}
	for _, value := range []string{tr("Pet packages (*.pet; *.zip)", "Пакеты питомцев (*.pet; *.zip)"), "*.pet;*.zip", tr("All files (*.*)", "Все файлы (*.*)"), "*.*"} {
		filter = append(filter, syscall.StringToUTF16(value)...)
	}
	filter = append(filter, 0)
	dialog := openFileName{
		Size: uint32(unsafe.Sizeof(openFileName{})), Owner: hwnd, Filter: &filter[0], FilterIndex: 1,
		File: &buffer[0], MaxFile: uint32(len(buffer)), Title: utf16(tr("Load pet", "Загрузить питомца")),
		Flags: 0x80000 | 0x1000 | 0x800 | 0x8, // Explorer, existing file and path, preserve working directory.
	}
	ok, _, _ := selectFile.Call(uintptr(unsafe.Pointer(&dialog)))
	runtime.KeepAlive(filter)
	runtime.KeepAlive(buffer)
	if ok == 0 {
		code, _, _ := fileDialogError.Call()
		if code != 0 {
			return "", fmt.Errorf("ошибка диалога выбора файла (0x%X)", code)
		}
		return "", nil
	}
	return syscall.UTF16ToString(buffer), nil
}

const startupKey = "Software\\Microsoft\\Windows\\CurrentVersion\\Run"
const currentUser = ^uintptr(0x7ffffffe)
const startupName = "YourPet"

func launchesAtLogin() bool {
	var size uint32
	status, _, _ := regGet.Call(currentUser, uintptr(unsafe.Pointer(utf16(startupKey))), uintptr(unsafe.Pointer(utf16(startupName))), 2, 0, 0, uintptr(unsafe.Pointer(&size)))
	return status == 0
}

func setLaunchAtLogin(enabled bool, executable string) error {
	subkey, name := utf16(startupKey), utf16(startupName)
	if !enabled {
		code, _, _ := regDelete.Call(currentUser, uintptr(unsafe.Pointer(subkey)), uintptr(unsafe.Pointer(name)))
		if code == 0 || code == 2 {
			return nil
		}
		return syscall.Errno(code)
	}
	command := syscall.StringToUTF16("\"" + executable + "\"")
	code, _, _ := regSet.Call(currentUser, uintptr(unsafe.Pointer(subkey)), uintptr(unsafe.Pointer(name)), 1,
		uintptr(unsafe.Pointer(&command[0])), uintptr(len(command)*2))
	runtime.KeepAlive(command)
	if code != 0 {
		return syscall.Errno(code)
	}
	return nil
}

type bitmapHeader struct {
	Size                   uint32
	Width, Height          int32
	Planes, Bits           uint16
	Compression, ImageSize uint32
	XPerMeter, YPerMeter   int32
	Colors, Important      uint32
}

type surface struct {
	dc, bitmap, previous uintptr
	width, height        int
	pixels               []byte
}

func (s *surface) close() {
	if s.dc != 0 {
		selectObject.Call(s.dc, s.previous)
		deleteObject.Call(s.bitmap)
		deleteDC.Call(s.dc)
	}
	*s = surface{}
}

func (s *surface) resize(width, height int) error {
	if s.width == width && s.height == height {
		return nil
	}
	next := surface{width: width, height: height}
	next.dc, _, _ = createDC.Call(0)
	if next.dc == 0 {
		return errors.New("не удалось создать контекст рисования")
	}
	var pixels *byte
	header := bitmapHeader{Size: 40, Width: int32(width), Height: -int32(height), Planes: 1, Bits: 32}
	next.bitmap, _, _ = createDIB.Call(next.dc, uintptr(unsafe.Pointer(&header)), 0, uintptr(unsafe.Pointer(&pixels)), 0, 0)
	if next.bitmap == 0 || pixels == nil {
		deleteDC.Call(next.dc)
		return errors.New("не удалось создать прозрачный кадр")
	}
	next.previous, _, _ = selectObject.Call(next.dc, next.bitmap)
	if next.previous == 0 || next.previous == ^uintptr(0) {
		deleteObject.Call(next.bitmap)
		deleteDC.Call(next.dc)
		return errors.New("не удалось открыть кадр для отрисовки")
	}
	next.pixels = unsafe.Slice(pixels, width*height*4)
	s.close()
	*s = next
	return nil
}

type sample struct {
	first, second int
	weight        uint32
}

func sampleAxis(target, source int) []sample {
	result := make([]sample, target)
	for index := range result {
		pos := math.Max(0, math.Min(float64(source-1), (float64(index)+0.5)*float64(source)/float64(target)-0.5))
		first := int(pos)
		result[index] = sample{first, min(source-1, first+1), uint32(math.Round((pos - float64(first)) * 256))}
	}
	return result
}

func (s *surface) draw(atlas image.Image, crop, fadeFrom image.Rectangle, fadeAlpha uint8) {
	width, height := crop.Dx(), crop.Dy()
	premul := make([][4]uint32, width*height)
	fromWeight := uint32(fadeAlpha)
	toWeight := 255 - fromWeight
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, g, b, a := atlas.At(crop.Min.X+x, crop.Min.Y+y).RGBA()
			if fadeAlpha != 0 {
				fr, fg, fb, fa := atlas.At(fadeFrom.Min.X+x, fadeFrom.Min.Y+y).RGBA()
				r = (r*toWeight + fr*fromWeight + 127) / 255
				g = (g*toWeight + fg*fromWeight + 127) / 255
				b = (b*toWeight + fb*fromWeight + 127) / 255
				a = (a*toWeight + fa*fromWeight + 127) / 255
			}
			premul[y*width+x] = [4]uint32{b >> 8, g >> 8, r >> 8, a >> 8}
		}
	}
	xs, ys := sampleAxis(s.width, width), sampleAxis(s.height, height)
	for y, vy := range ys {
		for x, vx := range xs {
			topLeft, topRight := premul[vy.first*width+vx.first], premul[vy.first*width+vx.second]
			bottomLeft, bottomRight := premul[vy.second*width+vx.first], premul[vy.second*width+vx.second]
			offset := (y*s.width + x) * 4
			for channel := 0; channel < 4; channel++ {
				top := topLeft[channel]*(256-vx.weight) + topRight[channel]*vx.weight
				bottom := bottomLeft[channel]*(256-vx.weight) + bottomRight[channel]*vx.weight
				s.pixels[offset+channel] = byte((top*(256-vy.weight) + bottom*vy.weight + 32768) >> 16)
			}
		}
	}
}

func (s *surface) present(hwnd uintptr, dest point) error {
	size := point{int32(s.width), int32(s.height)}
	source := point{}
	blend := [4]byte{0, 0, 255, 1}
	ok, _, err := updateLayeredWindow.Call(hwnd, 0, uintptr(unsafe.Pointer(&dest)), uintptr(unsafe.Pointer(&size)),
		s.dc, uintptr(unsafe.Pointer(&source)), 0, uintptr(unsafe.Pointer(&blend)), 2)
	if ok == 0 {
		return err
	}
	return nil
}
