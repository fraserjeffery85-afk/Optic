//go:build windows

package main

import (
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	pEnumDisplayDevicesW = user32.NewProc("EnumDisplayDevicesW")
	pMonitorFromWindow   = user32.NewProc("MonitorFromWindow")
	pGetMonitorInfoW     = user32.NewProc("GetMonitorInfoW")
	pGetAsyncKeyState    = user32.NewProc("GetAsyncKeyState")
	pIsWindowVisible     = user32.NewProc("IsWindowVisible")
	pCreateDCW           = gdi32.NewProc("CreateDCW")
	pDeleteDC            = gdi32.NewProc("DeleteDC")
	pGetDeviceGammaRamp  = gdi32.NewProc("GetDeviceGammaRamp")
	pSetDeviceGammaRamp  = gdi32.NewProc("SetDeviceGammaRamp")
	pShellExecuteW       = shell32.NewProc("ShellExecuteW")
	pCreateMutexW        = kernel32.NewProc("CreateMutexW")
	pRegisterHotKey      = user32.NewProc("RegisterHotKey")
	pGetMessageW         = user32.NewProc("GetMessageW")
)

type displayDevice struct {
	cb           uint32
	DeviceName   [32]uint16
	DeviceString [128]uint16
	StateFlags   uint32
	DeviceID     [128]uint16
	DeviceKey    [128]uint16
}

const displayAttachedToDesktop = 0x1

// listDisplays returns the GDI names (\\.\DISPLAY1 ...) of active displays.
func listDisplays() []string {
	var out []string
	for i := uint32(0); i < 32; i++ {
		var dd displayDevice
		dd.cb = uint32(unsafe.Sizeof(dd))
		r, _, _ := pEnumDisplayDevicesW.Call(0, uintptr(i), uintptr(unsafe.Pointer(&dd)), 0)
		if r == 0 {
			break
		}
		if dd.StateFlags&displayAttachedToDesktop != 0 {
			out = append(out, windows.UTF16ToString(dd.DeviceName[:]))
		}
	}
	return out
}

func withDisplayDC(name string, f func(hdc uintptr) bool) bool {
	drv, _ := windows.UTF16PtrFromString("DISPLAY")
	dev, _ := windows.UTF16PtrFromString(name)
	hdc, _, _ := pCreateDCW.Call(uintptr(unsafe.Pointer(drv)), uintptr(unsafe.Pointer(dev)), 0, 0)
	if hdc == 0 {
		return false
	}
	defer pDeleteDC.Call(hdc)
	return f(hdc)
}

func getRamp(name string) (r [3][256]uint16, ok bool) {
	ok = withDisplayDC(name, func(hdc uintptr) bool {
		ret, _, _ := pGetDeviceGammaRamp.Call(hdc, uintptr(unsafe.Pointer(&r)))
		return ret != 0
	})
	return
}

func setRamp(name string, r *[3][256]uint16) bool {
	return withDisplayDC(name, func(hdc uintptr) bool {
		ret, _, _ := pSetDeviceGammaRamp.Call(hdc, uintptr(unsafe.Pointer(r)))
		return ret != 0
	})
}

type monitorInfoEx struct {
	cbSize    uint32
	rcMonitor [4]int32
	rcWork    [4]int32
	dwFlags   uint32
	szDevice  [32]uint16
}

func monitorOfWindow(hwnd windows.HWND) string {
	const monitorDefaultToNearest = 2
	hm, _, _ := pMonitorFromWindow.Call(uintptr(hwnd), monitorDefaultToNearest)
	if hm == 0 {
		return ""
	}
	var mi monitorInfoEx
	mi.cbSize = uint32(unsafe.Sizeof(mi))
	r, _, _ := pGetMonitorInfoW.Call(hm, uintptr(unsafe.Pointer(&mi)))
	if r == 0 {
		return ""
	}
	return windows.UTF16ToString(mi.szDevice[:])
}

func processNameByPID(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 1024)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return ""
	}
	return strings.ToLower(filepath.Base(windows.UTF16ToString(buf[:n])))
}

// foregroundProcess returns the exe name and window of the focused app.
func foregroundProcess() (string, windows.HWND) {
	hwnd := windows.GetForegroundWindow()
	if hwnd == 0 {
		return "", 0
	}
	var pid uint32
	if _, err := windows.GetWindowThreadProcessId(hwnd, &pid); err != nil || pid == 0 {
		return "", 0
	}
	return processNameByPID(pid), hwnd
}

// runningProcesses returns the set of running exe names (lower case).
func runningProcesses() map[string]bool {
	set := map[string]bool{}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return set
	}
	defer windows.CloseHandle(snap)
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	if windows.Process32First(snap, &pe) != nil {
		return set
	}
	for {
		set[strings.ToLower(windows.UTF16ToString(pe.ExeFile[:]))] = true
		if windows.Process32Next(snap, &pe) != nil {
			break
		}
	}
	return set
}

// mainWindowOf finds a visible top-level window of a process (for "running" mode).
var (
	enumMu     sync.Mutex
	enumTarget string
	enumFound  windows.HWND
	enumCB     uintptr
	enumOnce   sync.Once
)

func mainWindowOf(exe string) windows.HWND {
	// syscall callbacks are a limited resource: create exactly one.
	enumOnce.Do(func() {
		enumCB = syscall.NewCallback(func(hwnd windows.HWND, _ uintptr) uintptr {
			vis, _, _ := pIsWindowVisible.Call(uintptr(hwnd))
			if vis == 0 {
				return 1
			}
			var pid uint32
			windows.GetWindowThreadProcessId(hwnd, &pid)
			if processNameByPID(pid) == enumTarget {
				enumFound = hwnd
				return 0
			}
			return 1
		})
	})
	enumMu.Lock()
	defer enumMu.Unlock()
	enumTarget, enumFound = exe, 0
	windows.EnumWindows(enumCB, nil)
	return enumFound
}

func keyDown(vk int) bool {
	r, _, _ := pGetAsyncKeyState.Call(uintptr(vk))
	return r&0x8000 != 0
}

func shellOpen(verb, file, args, dir string) error {
	v, _ := windows.UTF16PtrFromString(verb)
	f, _ := windows.UTF16PtrFromString(file)
	var a, d *uint16
	if args != "" {
		a, _ = windows.UTF16PtrFromString(args)
	}
	if dir != "" {
		d, _ = windows.UTF16PtrFromString(dir)
	}
	r, _, _ := pShellExecuteW.Call(0, uintptr(unsafe.Pointer(v)), uintptr(unsafe.Pointer(f)),
		uintptr(unsafe.Pointer(a)), uintptr(unsafe.Pointer(d)), 1)
	if r <= 32 {
		return syscall.Errno(r)
	}
	return nil
}

// singleInstance returns false if another copy is already running.
func singleInstance() bool {
	name, _ := windows.UTF16PtrFromString("Local\\OpticSingleInstance")
	h, _, err := pCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		return true
	}
	return err != windows.ERROR_ALREADY_EXISTS
}

type winMsg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      [2]int32
	private uint32
}

// registerHotkeys registers Ctrl+Alt+<vk> for each key on a dedicated OS
// thread and dispatches presses to f. Returns which keys were registered.
func registerHotkeys(vks []int, f func(vk int)) map[int]bool {
	const modAlt, modCtrl, modNoRepeat, wmHotkey = 0x1, 0x2, 0x4000, 0x0312
	result := make(chan map[int]bool)
	go func() {
		runtime.LockOSThread()
		ok := map[int]bool{}
		for i, vk := range vks {
			r, _, e := pRegisterHotKey.Call(0, uintptr(i+1), modAlt|modCtrl|modNoRepeat, uintptr(vk))
			ok[vk] = r != 0
			logf("hotkey Ctrl+Alt+0x%X registered=%v (%v)", vk, r != 0, e)
		}
		result <- ok
		any := false
		for _, v := range ok {
			any = any || v
		}
		if !any {
			return
		}
		var m winMsg
		for {
			r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if int32(r) <= 0 {
				return
			}
			if m.message == wmHotkey {
				logf("hotkey pressed id=%d", m.wParam)
				id := int(m.wParam) - 1
				if id >= 0 && id < len(vks) {
					go f(vks[id])
				}
			}
		}
	}()
	return <-result
}

const legacyExe = "clearride.exe"

// closeLegacyClearRide offers to close the old ClearRide if it's running:
// two copies changing the screen colours fight each other and flicker.
func closeLegacyClearRide() bool {
	if !runningProcesses()[legacyExe] {
		return false
	}
	if messageBox("ClearRide (the old version of Optic) is still running.\n\nRunning both makes the screen flicker, because they fight over your screen colours.\n\nClose ClearRide now?",
		appName, 0x4|0x30) != 6 {
		logf("legacy ClearRide left running (user said no)")
		return false
	}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(snap)
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	for err := windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		if strings.EqualFold(windows.UTF16ToString(pe.ExeFile[:]), legacyExe) {
			if h, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, pe.ProcessID); err == nil {
				windows.TerminateProcess(h, 0)
				windows.WaitForSingleObject(h, 3000)
				windows.CloseHandle(h)
				logf("closed legacy ClearRide (pid %d)", pe.ProcessID)
			}
		}
	}
	return true
}

const quitEventName = "Local\\OpticQuitEvent"

// waitForQuit runs f when another process (the installer) asks Optic to close.
func waitForQuit(f func()) {
	name, _ := windows.UTF16PtrFromString(quitEventName)
	h, err := windows.CreateEvent(nil, 1, 0, name)
	if err != nil {
		return
	}
	windows.WaitForSingleObject(h, windows.INFINITE)
	logf("asked to close by another process (installer)")
	f()
}

// signalQuit asks the running Optic to close (restoring the screen first).
func signalQuit() {
	name, _ := windows.UTF16PtrFromString(quitEventName)
	h, err := windows.OpenEvent(0x0002 /*EVENT_MODIFY_STATE*/, false, name)
	if err != nil {
		return
	}
	windows.SetEvent(h)
	windows.CloseHandle(h)
}
