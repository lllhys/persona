//go:build windows

package linktray

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The Win32 calls the tray makes that golang.org/x/sys/windows does not wrap, with their structures. Kept to what
// the tray uses.

var (
	user32  = windows.NewLazySystemDLL("user32.dll")
	shell32 = windows.NewLazySystemDLL("shell32.dll")
	dwmapi  = windows.NewLazySystemDLL("dwmapi.dll")
	gdi32   = windows.NewLazySystemDLL("gdi32.dll")
	shcore  = windows.NewLazySystemDLL("shcore.dll")

	procRegisterClassExW              = user32.NewProc("RegisterClassExW")
	procCreateWindowExW               = user32.NewProc("CreateWindowExW")
	procDefWindowProcW                = user32.NewProc("DefWindowProcW")
	procDestroyWindow                 = user32.NewProc("DestroyWindow")
	procShowWindow                    = user32.NewProc("ShowWindow")
	procSetWindowPos                  = user32.NewProc("SetWindowPos")
	procGetMessageW                   = user32.NewProc("GetMessageW")
	procTranslateMessage              = user32.NewProc("TranslateMessage")
	procDispatchMessageW              = user32.NewProc("DispatchMessageW")
	procPostMessageW                  = user32.NewProc("PostMessageW")
	procSendMessageW                  = user32.NewProc("SendMessageW")
	procPostQuitMessage               = user32.NewProc("PostQuitMessage")
	procLoadCursorW                   = user32.NewProc("LoadCursorW")
	procSetForegroundWindow           = user32.NewProc("SetForegroundWindow")
	procGetCursorPos                  = user32.NewProc("GetCursorPos")
	procCreatePopupMenu               = user32.NewProc("CreatePopupMenu")
	procAppendMenuW                   = user32.NewProc("AppendMenuW")
	procTrackPopupMenuEx              = user32.NewProc("TrackPopupMenuEx")
	procDestroyMenu                   = user32.NewProc("DestroyMenu")
	procRegisterWindowMessageW        = user32.NewProc("RegisterWindowMessageW")
	procFindWindowW                   = user32.NewProc("FindWindowW")
	procGetDpiForWindow               = user32.NewProc("GetDpiForWindow")
	procGetSystemMetricsForDpi        = user32.NewProc("GetSystemMetricsForDpi")
	procMonitorFromPoint              = user32.NewProc("MonitorFromPoint")
	procMonitorFromRect               = user32.NewProc("MonitorFromRect")
	procGetMonitorInfoW               = user32.NewProc("GetMonitorInfoW")
	procCreateIconIndirect            = user32.NewProc("CreateIconIndirect")
	procDestroyIcon                   = user32.NewProc("DestroyIcon")
	procSetTimer                      = user32.NewProc("SetTimer")
	procKillTimer                     = user32.NewProc("KillTimer")
	procSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	procIsWindowVisible               = user32.NewProc("IsWindowVisible")

	procShellNotifyIconW       = shell32.NewProc("Shell_NotifyIconW")
	procShellNotifyIconGetRect = shell32.NewProc("Shell_NotifyIconGetRect")
	procShellExecuteW          = shell32.NewProc("ShellExecuteW")

	procDwmSetWindowAttribute        = dwmapi.NewProc("DwmSetWindowAttribute")
	procDwmExtendFrameIntoClientArea = dwmapi.NewProc("DwmExtendFrameIntoClientArea")

	procCreateBitmap = gdi32.NewProc("CreateBitmap")
	procDeleteObject = gdi32.NewProc("DeleteObject")

	procGetDpiForMonitor = shcore.NewProc("GetDpiForMonitor")
)

const (
	wmDestroy          = 0x0002
	wmSize             = 0x0005
	wmActivate         = 0x0006
	wmClose            = 0x0010
	wmSettingChange    = 0x001A
	wmGetMinMaxInfo    = 0x0024
	wmCopyData         = 0x004A
	wmContextMenu      = 0x007B
	wmTimer            = 0x0113
	wmDpiChanged       = 0x02E0
	wmDwmColorChanged  = 0x0320
	wmApp              = 0x8000
	wmTrayIcon         = wmApp + 1 // the notification area's callback
	wmRun              = wmApp + 2 // run the queued calls on this thread
	wmSetIcon          = 0x0080
	waInactive         = 0
	ninSelect          = 0x0400
	ninKeySelect       = 0x0401
	ninBalloonUserClik = 0x0405

	wsPopup            = 0x80000000
	wsOverlappedWindow = 0x00CF0000
	wsMaximizeBox      = 0x00010000
	wsExToolWindow     = 0x00000080
	wsExTopmost        = 0x00000008
	wsExAppWindow      = 0x00040000

	swHide         = 0
	swShowNormal   = 1
	swShow         = 5
	swpNoSize      = 0x0001
	swpNoMove      = 0x0002
	swpNoZOrder    = 0x0004
	swpNoActivate  = 0x0010
	swpShowWindow  = 0x0040
	hwndTopmost    = ^uintptr(0) // -1
	cwUseDefault   = 0x80000000
	idcArrow       = 32512
	smCxSmIcon     = 49
	smCxIcon       = 11
	monitorNearest = 2

	nimAdd        = 0
	nimModify     = 1
	nimDelete     = 2
	nimSetVersion = 4
	nifMessage    = 0x01
	nifIcon       = 0x02
	nifTip        = 0x04
	nifInfo       = 0x10
	nifShowTip    = 0x80
	niifNone      = 0x00
	niifRespectQT = 0x80
	notifyVersion = 4

	mfString    = 0x0000
	mfGrayed    = 0x0001
	mfDisabled  = 0x0002
	mfChecked   = 0x0008
	mfSeparator = 0x0800
	tpmReturn   = 0x0100
	tpmRightBtn = 0x0002

	dwmwaUseImmersiveDarkMode = 20
	dwmwaCornerPreference     = 33
	dwmwaSystemBackdropType   = 38
	dwmwcpRound               = 2
	dwmwcpRoundSmall          = 3
	dwmsbtMainWindow          = 2 // Mica
	dwmsbtTransientWindow     = 3 // acrylic, for flyouts

	dpiAwarenessPerMonitorV2 = ^uintptr(3) // -4
)

type point struct{ X, Y int32 }

type rect struct{ Left, Top, Right, Bottom int32 }

func (r rect) width() int32  { return r.Right - r.Left }
func (r rect) height() int32 { return r.Bottom - r.Top }

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	private uint32
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type notifyIconData struct {
	Size            uint32
	Wnd             uintptr
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            uintptr
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Version         uint32 // union with uTimeout
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GUIDItem        windows.GUID
	BalloonIcon     uintptr
}

type notifyIconIdentifier struct {
	Size     uint32
	Wnd      uintptr
	ID       uint32
	GUIDItem windows.GUID
}

type monitorInfo struct {
	Size    uint32
	Monitor rect
	Work    rect
	Flags   uint32
}

type iconInfo struct {
	IsIcon   int32
	XHotspot uint32
	YHotspot uint32
	Mask     uintptr
	Color    uintptr
}

type margins struct{ Left, Right, Top, Bottom int32 }

type copyDataStruct struct {
	Data  uintptr
	Bytes uint32
	Ptr   uintptr
}

type minMaxInfo struct {
	Reserved     point
	MaxSize      point
	MaxPosition  point
	MinTrackSize point
	MaxTrackSize point
}

// fromLParam is the structure a message's lParam points at: the system hands pointers over as integers, and they
// stay valid for the message's handling.
func fromLParam(l uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&l))
}

func utf16Ptr(s string) *uint16 {
	p, _ := windows.UTF16PtrFromString(s)
	return p
}

// copyUTF16 fills dst with s, cut to fit with its terminating zero.
func copyUTF16(dst []uint16, s string) {
	u, _ := windows.UTF16FromString(s)
	if len(u) > len(dst) {
		u = u[:len(dst)]
		u[len(u)-1] = 0
	}
	copy(dst, u)
	for i := len(u); i < len(dst); i++ {
		dst[i] = 0
	}
}

func postMessage(hwnd uintptr, m uint32, w, l uintptr) {
	procPostMessageW.Call(hwnd, uintptr(m), w, l)
}

func setWindowPos(hwnd, after uintptr, x, y, w, h int32, flags uint32) {
	procSetWindowPos.Call(hwnd, after, uintptr(x), uintptr(y), uintptr(w), uintptr(h), uintptr(flags))
}

func windowVisible(hwnd uintptr) bool {
	r, _, _ := procIsWindowVisible.Call(hwnd)
	return r != 0
}

func dpiForWindow(hwnd uintptr) uint32 {
	if procGetDpiForWindow.Find() != nil {
		return 96
	}
	r, _, _ := procGetDpiForWindow.Call(hwnd)
	if r == 0 {
		return 96
	}
	return uint32(r)
}

// dpiForMonitor is the monitor's effective DPI; 96 when it cannot be read.
func dpiForMonitor(monitor uintptr) uint32 {
	var x, y uint32
	if procGetDpiForMonitor.Find() != nil {
		return 96
	}
	if r, _, _ := procGetDpiForMonitor.Call(monitor, 0, uintptr(unsafe.Pointer(&x)), uintptr(unsafe.Pointer(&y))); r != 0 || x == 0 {
		return 96
	}
	return x
}

func monitorInfoOf(monitor uintptr) (monitorInfo, bool) {
	mi := monitorInfo{Size: uint32(unsafe.Sizeof(monitorInfo{}))}
	r, _, _ := procGetMonitorInfoW.Call(monitor, uintptr(unsafe.Pointer(&mi)))
	return mi, r != 0
}

func cursorPos() point {
	var p point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	return p
}

// dwmSet sets a DWM attribute that is a 32-bit value; false when this Windows does not have it.
func dwmSet(hwnd uintptr, attr uint32, value uint32) bool {
	if procDwmSetWindowAttribute.Find() != nil {
		return false
	}
	r, _, _ := procDwmSetWindowAttribute.Call(hwnd, uintptr(attr), uintptr(unsafe.Pointer(&value)), 4)
	return r == 0
}

// shellOpen opens a file, folder or URL as Explorer would.
func shellOpen(target string) error {
	r, _, _ := procShellExecuteW.Call(0, uintptr(unsafe.Pointer(utf16Ptr("open"))), uintptr(unsafe.Pointer(utf16Ptr(target))), 0, 0, swShowNormal)
	if r <= 32 {
		return syscall.Errno(r)
	}
	return nil
}
