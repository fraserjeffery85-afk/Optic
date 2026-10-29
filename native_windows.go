//go:build windows

package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	comdlg32 = windows.NewLazySystemDLL("comdlg32.dll")

	pGetOpenFileNameW  = comdlg32.NewProc("GetOpenFileNameW")
	pGetSaveFileNameW  = comdlg32.NewProc("GetSaveFileNameW")
	pRegisterClassExW  = user32.NewProc("RegisterClassExW")
	pCreateWindowExW   = user32.NewProc("CreateWindowExW")
	pDefWindowProcW    = user32.NewProc("DefWindowProcW")
	pLoadImageW        = user32.NewProc("LoadImageW")
	pCreatePopupMenu   = user32.NewProc("CreatePopupMenu")
	pAppendMenuW       = user32.NewProc("AppendMenuW")
	pTrackPopupMenu    = user32.NewProc("TrackPopupMenu")
	pDestroyMenu       = user32.NewProc("DestroyMenu")
	pGetCursorPos      = user32.NewProc("GetCursorPos")
	pSetForegroundWnd  = user32.NewProc("SetForegroundWindow")
	pShowWindow        = user32.NewProc("ShowWindow")
	pFindWindowW       = user32.NewProc("FindWindowW")
	pMessageBoxW       = user32.NewProc("MessageBoxW")
	pSetWindowLongPtrW = user32.NewProc("SetWindowLongPtrW")
	pCallWindowProcW   = user32.NewProc("CallWindowProcW")
	pPostMessageW      = user32.NewProc("PostMessageW")
	pRegisterWindowMsg = user32.NewProc("RegisterWindowMessageW")
	pGetDpiForSystem   = user32.NewProc("GetDpiForSystem")
	pSystemParamsInfoW = user32.NewProc("SystemParametersInfoW")
	pSetWindowPos      = user32.NewProc("SetWindowPos")
	pShellNotifyIconW  = shell32.NewProc("Shell_NotifyIconW")
	pGetModuleHandleW  = kernel32.NewProc("GetModuleHandleW")
)

const (
	swHide    = 0
	swShow    = 5
	swRestore = 9
)

func u16(s string) *uint16 { p, _ := windows.UTF16PtrFromString(s); return p }

func messageBox(text, title string, flags uintptr) int {
	r, _, _ := pMessageBoxW.Call(0, uintptr(unsafe.Pointer(u16(text))), uintptr(unsafe.Pointer(u16(title))), flags)
	return int(r)
}

func showWindow(hwnd uintptr, cmd uintptr) { pShowWindow.Call(hwnd, cmd) }

func bringToFront(hwnd uintptr) {
	pShowWindow.Call(hwnd, swRestore)
	pShowWindow.Call(hwnd, swShow)
	pSetForegroundWnd.Call(hwnd)
}

func findOurWindow() uintptr {
	r, _, _ := pFindWindowW.Call(uintptr(unsafe.Pointer(u16("webview"))), uintptr(unsafe.Pointer(u16(appName))))
	return r
}

func systemDPI() int {
	if pGetDpiForSystem.Find() != nil {
		return 96
	}
	r, _, _ := pGetDpiForSystem.Call()
	if r == 0 {
		return 96
	}
	return int(r)
}

// workArea returns the size of the primary screen minus the taskbar.
func workArea() (w, h int) {
	var rc [4]int32
	const spiGetWorkArea = 0x30
	pSystemParamsInfoW.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&rc)), 0)
	return int(rc[2] - rc[0]), int(rc[3] - rc[1])
}

func centerWindow(hwnd uintptr, w, h int) {
	aw, ah := workArea()
	if w > aw {
		w = aw
	}
	if h > ah {
		h = ah
	}
	const swpNoZOrder = 0x4
	pSetWindowPos.Call(hwnd, 0, uintptr((aw-w)/2), uintptr((ah-h)/2), uintptr(w), uintptr(h), swpNoZOrder)
}

/* ------------------------------ file dialog ------------------------------ */

type openFileName struct {
	lStructSize       uint32
	hwndOwner         uintptr
	hInstance         uintptr
	lpstrFilter       *uint16
	lpstrCustomFilter *uint16
	nMaxCustFilter    uint32
	nFilterIndex      uint32
	lpstrFile         *uint16
	nMaxFile          uint32
	lpstrFileTitle    *uint16
	nMaxFileTitle     uint32
	lpstrInitialDir   *uint16
	lpstrTitle        *uint16
	flags             uint32
	nFileOffset       uint16
	nFileExtension    uint16
	lpstrDefExt       *uint16
	lCustData         uintptr
	lpfnHook          uintptr
	lpTemplateName    *uint16
	pvReserved        uintptr
	dwReserved        uint32
	flagsEx           uint32
}

// openFileDialog shows the standard Windows "Open" box. filter pairs are
// display name and pattern, e.g. {"Programs", "*.exe"}.
func openFileDialog(owner uintptr, title string, filter [][2]string, initialDir string) (string, bool) {
	var f []uint16
	for _, p := range filter {
		a, _ := windows.UTF16FromString(p[0])
		b, _ := windows.UTF16FromString(p[1])
		f = append(f, a...)
		f = append(f, b...)
	}
	f = append(f, 0)
	buf := make([]uint16, 1024)
	ofn := openFileName{
		hwndOwner:   owner,
		lpstrFilter: &f[0],
		lpstrFile:   &buf[0],
		nMaxFile:    uint32(len(buf)),
		lpstrTitle:  u16(title),
		flags:       0x00001000 | 0x00000800 | 0x00080000 | 0x00000008, // FILEMUSTEXIST|PATHMUSTEXIST|EXPLORER|NOCHANGEDIR
	}
	if initialDir != "" {
		ofn.lpstrInitialDir = u16(initialDir)
	}
	ofn.lStructSize = uint32(unsafe.Sizeof(ofn))
	r, _, _ := pGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	if r == 0 {
		return "", false
	}
	return windows.UTF16ToString(buf), true
}

// saveFileDialog shows the standard "Save as" box.
func saveFileDialog(owner uintptr, title, defaultName string, filter [][2]string, defExt string) (string, bool) {
	var f []uint16
	for _, p := range filter {
		a, _ := windows.UTF16FromString(p[0])
		b, _ := windows.UTF16FromString(p[1])
		f = append(f, a...)
		f = append(f, b...)
	}
	f = append(f, 0)
	buf := make([]uint16, 1024)
	d, _ := windows.UTF16FromString(defaultName)
	copy(buf, d)
	ofn := openFileName{
		hwndOwner:   owner,
		lpstrFilter: &f[0],
		lpstrFile:   &buf[0],
		nMaxFile:    uint32(len(buf)),
		lpstrTitle:  u16(title),
		lpstrDefExt: u16(defExt),
		flags:       0x00000002 | 0x00000800 | 0x00080000 | 0x00000008, // OVERWRITEPROMPT|PATHMUSTEXIST|EXPLORER|NOCHANGEDIR
	}
	ofn.lStructSize = uint32(unsafe.Sizeof(ofn))
	r, _, _ := pGetSaveFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	if r == 0 {
		return "", false
	}
	return windows.UTF16ToString(buf), true
}

/* ------------------------------ tray icon ------------------------------- */

type notifyIconData struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         windows.GUID
	HBalloonIcon     uintptr
}

const (
	nimAdd, nimModify, nimDelete = 0, 1, 2
	nifMessage, nifIcon, nifTip  = 1, 2, 4
	nifInfo                      = 0x10
	wmTray                       = 0x0400 + 77 // WM_USER+77 (not WM_APP: the webview loop uses that)
	wmLButtonUp, wmRButtonUp     = 0x0202, 0x0205
	wmContextMenu                = 0x007B
	mfString, mfSeparator        = 0, 0x800
	mfChecked                    = 0x8
	tpmReturnCmd, tpmRightButton = 0x100, 0x2
)

type Tray struct {
	hwnd           uintptr
	nid            notifyIconData
	taskbarCreated uint32
	OnOpen         func()
	OnMenu         func() []TrayItem // built fresh each time it opens
}

type TrayItem struct {
	Text    string
	Checked bool
	Action  func()
}

var theTray *Tray

func NewTray(tooltip string) *Tray {
	t := &Tray{}
	theTray = t
	hinst, _, _ := pGetModuleHandleW.Call(0)
	cls := u16("OpticTrayWindow")
	type wndClassEx struct {
		cbSize        uint32
		style         uint32
		lpfnWndProc   uintptr
		cbClsExtra    int32
		cbWndExtra    int32
		hInstance     uintptr
		hIcon         uintptr
		hCursor       uintptr
		hbrBackground uintptr
		lpszMenuName  *uint16
		lpszClassName *uint16
		hIconSm       uintptr
	}
	wc := wndClassEx{lpfnWndProc: syscall.NewCallback(trayProc), hInstance: hinst, lpszClassName: cls}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	t.hwnd, _, _ = pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(u16(appName+" tray"))),
		0, 0, 0, 0, 0, 0, 0, hinst, 0)
	t.taskbarCreated = uint32(func() uintptr {
		r, _, _ := pRegisterWindowMsg.Call(uintptr(unsafe.Pointer(u16("TaskbarCreated"))))
		return r
	}())

	icon, _, _ := pLoadImageW.Call(hinst, 1, 1 /*IMAGE_ICON*/, 0, 0, 0x40|0x8000 /*DEFAULTSIZE|SHARED*/)
	t.nid = notifyIconData{HWnd: t.hwnd, UID: 1, UFlags: nifMessage | nifIcon | nifTip, UCallbackMessage: wmTray, HIcon: icon}
	t.nid.CbSize = uint32(unsafe.Sizeof(t.nid))
	t.setTip(tooltip)
	pShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&t.nid)))
	return t
}

func (t *Tray) setTip(s string) {
	u, _ := windows.UTF16FromString(s)
	if len(u) > 127 {
		u = append(u[:127], 0)
	}
	t.nid.SzTip = [128]uint16{}
	copy(t.nid.SzTip[:], u)
}

func (t *Tray) SetTooltip(s string) {
	t.setTip(s)
	t.nid.UFlags = nifMessage | nifIcon | nifTip
	pShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&t.nid)))
}

func (t *Tray) Notify(title, text string) {
	n := t.nid
	n.UFlags = nifInfo
	tt, _ := windows.UTF16FromString(title)
	tx, _ := windows.UTF16FromString(text)
	copy(n.SzInfoTitle[:63], tt)
	copy(n.SzInfo[:255], tx)
	n.DwInfoFlags = 1 // NIIF_INFO
	pShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&n)))
}

func (t *Tray) Remove() { pShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&t.nid))) }

func (t *Tray) showMenu() {
	if t.OnMenu == nil {
		return
	}
	items := t.OnMenu()
	m, _, _ := pCreatePopupMenu.Call()
	for i, it := range items {
		if it.Text == "" {
			pAppendMenuW.Call(m, mfSeparator, 0, 0)
			continue
		}
		flags := uintptr(mfString)
		if it.Checked {
			flags |= mfChecked
		}
		pAppendMenuW.Call(m, flags, uintptr(i+1), uintptr(unsafe.Pointer(u16(it.Text))))
	}
	var pt [2]int32
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWnd.Call(t.hwnd)
	cmd, _, _ := pTrackPopupMenu.Call(m, tpmReturnCmd|tpmRightButton, uintptr(pt[0]), uintptr(pt[1]), 0, t.hwnd, 0)
	pPostMessageW.Call(t.hwnd, 0, 0, 0) // WM_NULL: makes the menu close properly
	pDestroyMenu.Call(m)
	if cmd > 0 && int(cmd) <= len(items) && items[cmd-1].Action != nil {
		items[cmd-1].Action()
	}
}

func trayProc(hwnd, msg, wp, lp uintptr) uintptr {
	t := theTray
	if t != nil {
		switch {
		case msg == wmTray:
			switch lp & 0xFFFF {
			case wmLButtonUp:
				if t.OnOpen != nil {
					t.OnOpen()
				}
			case wmRButtonUp, wmContextMenu:
				t.showMenu()
			}
			return 0
		case t.taskbarCreated != 0 && uint32(msg) == t.taskbarCreated: // Explorer restarted
			pShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&t.nid)))
			return 0
		}
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wp, lp)
	return r
}

/* ----------------- keep the window alive when "closed" ------------------ */

var (
	oldWndProc uintptr
	onCloseFn  func() bool // return true to allow real close
)

const gwlpWndProc = ^uintptr(3) // -4

func subclassClose(hwnd uintptr, onClose func() bool) {
	onCloseFn = onClose
	cb := syscall.NewCallback(func(h, msg, wp, lp uintptr) uintptr {
		if msg == 0x0010 /*WM_CLOSE*/ && onCloseFn != nil && !onCloseFn() {
			return 0
		}
		r, _, _ := pCallWindowProcW.Call(oldWndProc, h, msg, wp, lp)
		return r
	})
	oldWndProc, _, _ = pSetWindowLongPtrW.Call(hwnd, gwlpWndProc, cb)
}

/* ------------------------------ screenshots ----------------------------- */

func documentsDir() string {
	p, err := windows.KnownFolderPath(windows.FOLDERID_Documents, 0)
	if err != nil {
		h, _ := os.UserHomeDir()
		return filepath.Join(h, "Documents")
	}
	return p
}

// latestScreenshot finds the newest picture from any configured game:
// MX Bikes' own screenshot folder, plus pictures saved in each game folder
// (ReShade saves there by default).
func latestScreenshot(games []Game) string {
	var dirs []string
	dirs = append(dirs, filepath.Join(documentsDir(), "PiBoSo", "MX Bikes", "scrshots"))
	for _, g := range games {
		if g.Folder != "" {
			dirs = append(dirs, g.Folder, filepath.Join(g.Folder, "screenshots"))
		}
	}
	type cand struct {
		path string
		mod  int64
	}
	var all []cand
	for _, d := range dirs {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			n := strings.ToLower(e.Name())
			if e.IsDir() || !(strings.HasSuffix(n, ".png") || strings.HasSuffix(n, ".jpg") || strings.HasSuffix(n, ".jpeg")) {
				continue
			}
			if fi, err := e.Info(); err == nil && fi.Size() > 20000 {
				all = append(all, cand{filepath.Join(d, e.Name()), fi.ModTime().UnixNano()})
			}
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].mod > all[j].mod })
	for _, c := range all {
		if d := imageDataURL(c.path); d != "" {
			return d
		}
	}
	return ""
}

// imageDataURL loads a picture, shrinks it to at most 1280 px wide and returns
// it as a JPEG data URL for the preview.
func imageDataURL(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	src, _, err := image.Decode(f)
	if err != nil {
		return ""
	}
	img := shrink(src, 1280)
	var buf bytes.Buffer
	if jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}) != nil {
		return ""
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

// shrink box-filters an image down to maxW wide (no-op if already smaller).
func shrink(src image.Image, maxW int) image.Image {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw <= maxW {
		return src
	}
	dw := maxW
	dh := sh * dw / sw
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		y0, y1 := y*sh/dh, (y+1)*sh/dh
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < dw; x++ {
			x0, x1 := x*sw/dw, (x+1)*sw/dw
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var r, g, bl, n uint32
			for yy := y0; yy < y1; yy += 1 + (y1-y0)/3 {
				for xx := x0; xx < x1; xx += 1 + (x1-x0)/3 {
					cr, cg, cb, _ := src.At(b.Min.X+xx, b.Min.Y+yy).RGBA()
					r, g, bl, n = r+cr, g+cg, bl+cb, n+1
				}
			}
			i := dst.PixOffset(x, y)
			dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3] = uint8(r/n>>8), uint8(g/n>>8), uint8(bl/n>>8), 255
		}
	}
	return dst
}
