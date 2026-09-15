//go:build windows

// Package aumid registers this app's AppUserModelID via a Start Menu
// shortcut.
//
// Windows requires a classic (non-packaged) desktop app to have an
// AppUserModelID backed by a Start Menu shortcut carrying that ID as an
// explicit property before it's treated as a first-class, "installed"
// notification source. Without it, Windows can't resolve a display
// name/icon/settings entry for the app, and toast notifications tend to
// get filed straight into Action Center without ever showing the
// on-screen banner - which is exactly the symptom this package fixes.
package aumid

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/go-ole/go-ole"
	"github.com/moutend/go-wca/pkg/wca"
)

// ID is the AppUserModelID used both for the Start Menu shortcut and as
// the argument to CreateToastNotifier in internal/notifier.
const ID = "SpikePy.AudioOutputSwitcher"

var (
	clsidShellLink   = ole.NewGUID("{00021401-0000-0000-C000-000000000046}")
	iidShellLinkW    = ole.NewGUID("{000214F9-0000-0000-C000-000000000046}")
	iidPersistFile   = ole.NewGUID("{0000010b-0000-0000-C000-000000000046}")
	iidPropertyStore = ole.NewGUID("{886D8EEB-8CF2-4446-8D02-CDBA1DBDCF99}")
)

var pkeyAppUserModelID = wca.DefinePropertyKey(
	0x9F4C2855, 0x9F79, 0x4B39, 0xA8, 0xD0, 0xE1, 0xD4, 0x2D, 0xE1, 0xD5, 0xF3, 5,
)

var (
	modole32           = syscall.NewLazyDLL("ole32.dll")
	procCoTaskMemAlloc = modole32.NewProc("CoTaskMemAlloc")
	procCoTaskMemFree  = modole32.NewProc("CoTaskMemFree")
)

// ShortcutPath is where the registering Start Menu shortcut lives. It's
// a normal, user-visible entry (it will show up in Start Menu search) -
// that visibility is inherent to how Windows resolves AppUserModelIDs
// for classic desktop apps. Exported so the uninstaller can remove it.
func ShortcutPath() string {
	return filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs", "Audio Output Switcher.lnk")
}

// Ensure creates or refreshes the Start Menu shortcut pointing at
// exePath with the AppUserModelID property set. Safe to call on every
// startup.
func Ensure(exePath string) error {
	var result error
	done := make(chan struct{})
	go func() {
		defer close(done)
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err != nil {
			result = err
			return
		}
		defer ole.CoUninitialize()

		result = ensureShortcut(exePath)
	}()
	<-done
	return result
}

func ensureShortcut(exePath string) error {
	var link *iShellLink
	if err := wca.CoCreateInstance(clsidShellLink, 0, wca.CLSCTX_ALL, iidShellLinkW, &link); err != nil {
		return fmt.Errorf("create shell link: %w", err)
	}
	defer link.Release()

	if err := link.setPath(exePath); err != nil {
		return fmt.Errorf("set shell link path: %w", err)
	}

	var store *iPropertyStore
	if err := link.PutQueryInterface(iidPropertyStore, &store); err != nil {
		return fmt.Errorf("get property store: %w", err)
	}
	defer store.Release()

	pv, free, err := newStringPropVariant(ID)
	if err != nil {
		return fmt.Errorf("build AppUserModelID value: %w", err)
	}
	defer free()

	if err := store.SetValue(&pkeyAppUserModelID, pv); err != nil {
		return fmt.Errorf("set AppUserModelID: %w", err)
	}
	if err := store.Commit(); err != nil {
		return fmt.Errorf("commit property store: %w", err)
	}

	var persist *iPersistFile
	if err := link.PutQueryInterface(iidPersistFile, &persist); err != nil {
		return fmt.Errorf("get persist file: %w", err)
	}
	defer persist.Release()

	if err := os.MkdirAll(filepath.Dir(ShortcutPath()), 0o755); err != nil {
		return fmt.Errorf("create start menu folder: %w", err)
	}
	if err := persist.save(ShortcutPath()); err != nil {
		return fmt.Errorf("save shortcut: %w", err)
	}
	return nil
}

const vtLPWSTR = 31

// newStringPropVariant builds a VT_LPWSTR PROPVARIANT for s. The
// returned free func must be called once the value is no longer needed
// (IPropertyStore.SetValue copies it internally, so it's safe to free
// right after that call returns).
func newStringPropVariant(s string) (*ole.VARIANT, func(), error) {
	u16, err := syscall.UTF16FromString(s)
	if err != nil {
		return nil, nil, err
	}

	size := uintptr(len(u16) * 2)
	ptr, _, _ := procCoTaskMemAlloc.Call(size)
	if ptr == 0 {
		return nil, nil, fmt.Errorf("CoTaskMemAlloc failed")
	}
	dst := unsafe.Slice((*uint16)(unsafe.Pointer(ptr)), len(u16))
	copy(dst, u16)

	pv := &ole.VARIANT{VT: ole.VT(vtLPWSTR), Val: int64(ptr)}
	free := func() { procCoTaskMemFree.Call(ptr) }
	return pv, free, nil
}

// iShellLink is the documented, stable-since-Windows-95 IShellLinkW COM
// interface, used here only to set the target path.
type iShellLink struct {
	ole.IUnknown
}

type iShellLinkVtbl struct {
	ole.IUnknownVtbl
	GetPath             uintptr
	GetIDList           uintptr
	SetIDList           uintptr
	GetDescription      uintptr
	SetDescription      uintptr
	GetWorkingDirectory uintptr
	SetWorkingDirectory uintptr
	GetArguments        uintptr
	SetArguments        uintptr
	GetHotkey           uintptr
	SetHotkey           uintptr
	GetShowCmd          uintptr
	SetShowCmd          uintptr
	GetIconLocation     uintptr
	SetIconLocation     uintptr
	SetRelativePath     uintptr
	Resolve             uintptr
	SetPath             uintptr
}

func (v *iShellLink) vtable() *iShellLinkVtbl {
	return (*iShellLinkVtbl)(unsafe.Pointer(v.RawVTable))
}

func (v *iShellLink) setPath(path string) error {
	ptr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	hr, _, _ := syscall.SyscallN(v.vtable().SetPath, uintptr(unsafe.Pointer(v)), uintptr(unsafe.Pointer(ptr)))
	if hr != 0 {
		return ole.NewError(hr)
	}
	return nil
}

// iPersistFile is the documented IPersistFile COM interface, used here
// only to save the shell link to a .lnk file.
type iPersistFile struct {
	ole.IUnknown
}

type iPersistFileVtbl struct {
	ole.IUnknownVtbl
	GetClassID    uintptr
	IsDirty       uintptr
	Load          uintptr
	Save          uintptr
	SaveCompleted uintptr
	GetCurFile    uintptr
}

func (v *iPersistFile) vtable() *iPersistFileVtbl {
	return (*iPersistFileVtbl)(unsafe.Pointer(v.RawVTable))
}

func (v *iPersistFile) save(path string) error {
	ptr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	hr, _, _ := syscall.SyscallN(v.vtable().Save, uintptr(unsafe.Pointer(v)), uintptr(unsafe.Pointer(ptr)), 1)
	if hr != 0 {
		return ole.NewError(hr)
	}
	return nil
}

// iPropertyStore is the documented IPropertyStore COM interface. It's
// redefined here (rather than reusing go-wca's read-only IPropertyStore)
// because this needs a working SetValue/Commit, which go-wca's version
// stubs out.
type iPropertyStore struct {
	ole.IUnknown
}

type iPropertyStoreVtbl struct {
	ole.IUnknownVtbl
	GetCount uintptr
	GetAt    uintptr
	GetValue uintptr
	SetValue uintptr
	Commit   uintptr
}

func (v *iPropertyStore) vtable() *iPropertyStoreVtbl {
	return (*iPropertyStoreVtbl)(unsafe.Pointer(v.RawVTable))
}

func (v *iPropertyStore) SetValue(key *wca.PROPERTYKEY, pv *ole.VARIANT) error {
	hr, _, _ := syscall.SyscallN(v.vtable().SetValue, uintptr(unsafe.Pointer(v)), uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(pv)))
	if hr != 0 {
		return ole.NewError(hr)
	}
	return nil
}

func (v *iPropertyStore) Commit() error {
	hr, _, _ := syscall.SyscallN(v.vtable().Commit, uintptr(unsafe.Pointer(v)))
	if hr != 0 {
		return ole.NewError(hr)
	}
	return nil
}
