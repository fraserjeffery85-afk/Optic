//go:build windows

package main

import (
	"errors"
	"fmt"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Saturator boosts colour saturation through the graphics driver. Gamma ramps
// can't change saturation (it mixes channels), so this needs vendor APIs:
// NVIDIA "digital vibrance" (NVAPI) or AMD display colour (ADL).
type Saturator interface {
	Name() string
	// Apply sets saturation boost pct (0..100) on the given displays,
	// remembering each display's original value the first time.
	Apply(displays []string, pct float64) error
	// Restore puts every touched display back to its original value.
	Restore()
	// Originals returns what Restore would write (for crash recovery).
	Originals() map[string]int
	RestoreFrom(m map[string]int)
}

func detectSaturator() Saturator {
	nv, err := newNvidia()
	if err == nil {
		return nv
	}
	logf("NVIDIA saturation unavailable: %v", err)
	amd, err := newAMD()
	if err == nil {
		return amd
	}
	logf("AMD saturation unavailable: %v", err)
	return nil
}

// safeCall guards against a misbehaving driver DLL taking the app down.
func safeCall(f func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("driver call failed: %v", r)
		}
	}()
	return f()
}

/* ---------------------------- NVIDIA (NVAPI) ---------------------------- */

type nvidia struct {
	enumHandle  uintptr
	assocHandle uintptr
	getDVC      uintptr
	setDVC      uintptr
	orig        map[string]int32 // display name -> original level
}

type nvDVCInfo struct {
	Version uint32
	Current int32
	Min     int32
	Max     int32
}

func newNvidia() (*nvidia, error) {
	dll := windows.NewLazySystemDLL("nvapi64.dll")
	if err := dll.Load(); err != nil {
		return nil, err
	}
	qi := dll.NewProc("nvapi_QueryInterface")
	if qi.Find() != nil {
		return nil, errors.New("nvapi_QueryInterface missing")
	}
	q := func(id uint32) uintptr { r, _, _ := qi.Call(uintptr(id)); return r }
	n := &nvidia{
		enumHandle:  q(0x9ABDD40D),
		assocHandle: q(0x35C29134),
		getDVC:      q(0x4085DE45),
		setDVC:      q(0x172409B4),
		orig:        map[string]int32{},
	}
	init := q(0x0150E828)
	if init == 0 || n.assocHandle == 0 || n.getDVC == 0 || n.setDVC == 0 {
		return nil, errors.New("NVAPI functions unavailable")
	}
	if r, _, _ := syscall.SyscallN(init); r != 0 {
		return nil, fmt.Errorf("NvAPI_Initialize failed (%d)", int32(r))
	}
	return n, nil
}

func (n *nvidia) Name() string { return "NVIDIA digital vibrance" }

func (n *nvidia) handleFor(display string) (uintptr, bool) {
	name, err := windows.BytePtrFromString(display)
	if err != nil {
		return 0, false
	}
	var h uintptr
	r, _, _ := syscall.SyscallN(n.assocHandle, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&h)))
	return h, r == 0 && h != 0
}

func (n *nvidia) info(h uintptr) (nvDVCInfo, bool) {
	var inf nvDVCInfo
	inf.Version = uint32(unsafe.Sizeof(inf)) | 1<<16
	r, _, _ := syscall.SyscallN(n.getDVC, h, 0, uintptr(unsafe.Pointer(&inf)))
	return inf, r == 0
}

func (n *nvidia) set(h uintptr, level int32) bool {
	r, _, _ := syscall.SyscallN(n.setDVC, h, 0, uintptr(level))
	return r == 0
}

func (n *nvidia) Apply(displays []string, pct float64) error {
	return safeCall(func() error {
		var failed []string
		for _, d := range displays {
			h, ok := n.handleFor(d)
			if !ok {
				continue // not an NVIDIA-driven display
			}
			inf, ok := n.info(h)
			if !ok {
				failed = append(failed, d)
				continue
			}
			base, seen := n.orig[d]
			if !seen {
				base = inf.Current
				n.orig[d] = base
			}
			target := base + int32(float64(inf.Max-base)*pct/100+0.5)
			if target != inf.Current && !n.set(h, target) {
				failed = append(failed, d)
			}
		}
		if len(failed) > 0 {
			return errors.New("vibrance not accepted on " + strings.Join(failed, ", "))
		}
		return nil
	})
}

func (n *nvidia) Restore() {
	_ = safeCall(func() error {
		for d, lvl := range n.orig {
			if h, ok := n.handleFor(d); ok {
				n.set(h, lvl)
			}
		}
		n.orig = map[string]int32{}
		return nil
	})
}

func (n *nvidia) Originals() map[string]int {
	m := map[string]int{}
	for k, v := range n.orig {
		m[k] = int(v)
	}
	return m
}

func (n *nvidia) RestoreFrom(m map[string]int) {
	_ = safeCall(func() error {
		for d, lvl := range m {
			if h, ok := n.handleFor(d); ok {
				n.set(h, int32(lvl))
			}
		}
		return nil
	})
}

/* ------------------------------ AMD (ADL) ------------------------------- */

type amdTarget struct{ adapter, display int32 }

type amd struct {
	numAdapters, adapterInfo, displayInfo, colorGet, colorSet *windows.LazyProc
	free                                                      *windows.LazyProc
	orig                                                      map[string]int32  // "adapter:display" -> original
	names                                                     map[string]string // key -> GDI display name
}

const (
	adlColorSaturation    = 4
	adlAdapterInfoSize    = 1572
	adlAdapterInfoName    = 536 // offset of strDisplayName
	adlAdapterInfoPresent = 792
	adlDisplayInfoSize    = 552
	adlDisplayValueOff    = 548 // iDisplayInfoValue
	adlDisplayConnected   = 0x1
	adlDisplayMapped      = 0x2
)

func newAMD() (*amd, error) {
	dll := windows.NewLazySystemDLL("atiadlxx.dll")
	if err := dll.Load(); err != nil {
		return nil, err
	}
	crt := windows.NewLazySystemDLL("msvcrt.dll")
	malloc := crt.NewProc("malloc")
	if err := malloc.Find(); err != nil {
		return nil, err
	}
	a := &amd{
		numAdapters: dll.NewProc("ADL_Adapter_NumberOfAdapters_Get"),
		adapterInfo: dll.NewProc("ADL_Adapter_AdapterInfo_Get"),
		displayInfo: dll.NewProc("ADL_Display_DisplayInfo_Get"),
		colorGet:    dll.NewProc("ADL_Display_Color_Get"),
		colorSet:    dll.NewProc("ADL_Display_Color_Set"),
		free:        crt.NewProc("free"),
		orig:        map[string]int32{},
		names:       map[string]string{},
	}
	create := dll.NewProc("ADL_Main_Control_Create")
	for _, p := range []*windows.LazyProc{create, a.numAdapters, a.adapterInfo, a.displayInfo, a.colorGet, a.colorSet} {
		if err := p.Find(); err != nil {
			return nil, err
		}
	}
	// On x64 there is one calling convention, so the CRT's malloc can be
	// handed straight to ADL as its allocation callback.
	if r, _, _ := create.Call(malloc.Addr(), 1); int32(r) != 0 {
		return nil, fmt.Errorf("ADL init failed (%d)", int32(r))
	}
	return a, nil
}

func (a *amd) Name() string { return "AMD display saturation" }

// targets maps GDI display names to ADL (adapter, display) pairs.
func (a *amd) targets(wanted map[string]bool) map[string]amdTarget {
	out := map[string]amdTarget{}
	var n int32
	if r, _, _ := a.numAdapters.Call(uintptr(unsafe.Pointer(&n))); int32(r) != 0 || n <= 0 || n > 64 {
		return out
	}
	buf := make([]byte, int(n)*adlAdapterInfoSize)
	for i := 0; i < int(n); i++ {
		*(*int32)(unsafe.Pointer(&buf[i*adlAdapterInfoSize])) = adlAdapterInfoSize
	}
	if r, _, _ := a.adapterInfo.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf))); int32(r) != 0 {
		return out
	}
	for i := 0; i < int(n); i++ {
		rec := buf[i*adlAdapterInfoSize : (i+1)*adlAdapterInfoSize]
		adapterIndex := *(*int32)(unsafe.Pointer(&rec[4]))
		present := *(*int32)(unsafe.Pointer(&rec[adlAdapterInfoPresent]))
		name := cString(rec[adlAdapterInfoName : adlAdapterInfoName+256])
		if present == 0 || !wanted[strings.ToUpper(name)] {
			continue
		}
		if _, done := out[name]; done {
			continue
		}
		var count int32
		var ptr uintptr
		if r, _, _ := a.displayInfo.Call(uintptr(adapterIndex), uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&ptr)), 0); int32(r) != 0 || ptr == 0 {
			continue
		}
		for j := 0; j < int(count) && j < 32; j++ {
			base := ptr + uintptr(j*adlDisplayInfoSize)
			logical := *(*int32)(unsafe.Pointer(base))
			logicalAdapter := *(*int32)(unsafe.Pointer(base + 8))
			value := *(*int32)(unsafe.Pointer(base + adlDisplayValueOff))
			if value&(adlDisplayConnected|adlDisplayMapped) == adlDisplayConnected|adlDisplayMapped && logicalAdapter == adapterIndex {
				out[name] = amdTarget{adapterIndex, logical}
				break
			}
		}
		a.free.Call(ptr)
	}
	return out
}

func cString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

func (a *amd) get(t amdTarget) (cur, def, min, max int32, ok bool) {
	var step int32
	r, _, _ := a.colorGet.Call(uintptr(t.adapter), uintptr(t.display), adlColorSaturation,
		uintptr(unsafe.Pointer(&cur)), uintptr(unsafe.Pointer(&def)), uintptr(unsafe.Pointer(&min)),
		uintptr(unsafe.Pointer(&max)), uintptr(unsafe.Pointer(&step)))
	return cur, def, min, max, int32(r) == 0
}

func (a *amd) set(t amdTarget, v int32) bool {
	r, _, _ := a.colorSet.Call(uintptr(t.adapter), uintptr(t.display), adlColorSaturation, uintptr(v))
	return int32(r) == 0
}

func key(t amdTarget) string { return itoa(int(t.adapter)) + ":" + itoa(int(t.display)) }

func (a *amd) Apply(displays []string, pct float64) error {
	return safeCall(func() error {
		wanted := map[string]bool{}
		for _, d := range displays {
			wanted[strings.ToUpper(d)] = true
		}
		var failed []string
		for name, t := range a.targets(wanted) {
			cur, _, _, max, ok := a.get(t)
			if !ok {
				failed = append(failed, name)
				continue
			}
			k := key(t)
			base, seen := a.orig[k]
			if !seen {
				base = cur
				a.orig[k] = base
				a.names[k] = name
			}
			target := base + int32(float64(max-base)*pct/100+0.5)
			if target != cur && !a.set(t, target) {
				failed = append(failed, name)
			}
		}
		if len(failed) > 0 {
			return errors.New("saturation not accepted on " + strings.Join(failed, ", "))
		}
		return nil
	})
}

func (a *amd) Restore() {
	_ = safeCall(func() error {
		a.RestoreFrom(a.Originals())
		a.orig = map[string]int32{}
		return nil
	})
}

func (a *amd) Originals() map[string]int {
	m := map[string]int{}
	for k, v := range a.orig {
		m[k] = int(v)
	}
	return m
}

func (a *amd) RestoreFrom(m map[string]int) {
	_ = safeCall(func() error {
		for k, v := range m {
			var ad, di int
			if _, err := fmt.Sscanf(k, "%d:%d", &ad, &di); err == nil {
				a.set(amdTarget{int32(ad), int32(di)}, int32(v))
			}
		}
		return nil
	})
}
