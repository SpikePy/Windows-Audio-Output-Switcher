//go:build windows

package audio

import (
	"syscall"
	"unsafe"

	"github.com/go-ole/go-ole"
	"github.com/moutend/go-wca/pkg/wca"
)

// deviceChanges gets a signal whenever Windows reports an output device
// change. Its buffer of 1 coalesces a burst into one pending signal
// instead of ever blocking the Windows thread that delivers it.
var deviceChanges = make(chan struct{}, 1)

// notificationClient is a minimal IMMNotificationClient COM object that
// lives for the whole process, so it needs no reference counting. Windows
// calls it on its own threads; every method only does a non-blocking send.
type notificationClient struct {
	vtbl *notificationClientVtbl
}

type notificationClientVtbl struct {
	QueryInterface         uintptr
	AddRef                 uintptr
	Release                uintptr
	OnDeviceStateChanged   uintptr
	OnDeviceAdded          uintptr
	OnDeviceRemoved        uintptr
	OnDefaultDeviceChanged uintptr
	OnPropertyValueChanged uintptr
}

var client = &notificationClient{vtbl: &notificationClientVtbl{
	QueryInterface:         syscall.NewCallback(ncQueryInterface),
	AddRef:                 syscall.NewCallback(ncAddRefRelease),
	Release:                syscall.NewCallback(ncAddRefRelease),
	OnDeviceStateChanged:   syscall.NewCallback(ncOnDeviceStateChanged),
	OnDeviceAdded:          syscall.NewCallback(ncOnDeviceAddedRemoved),
	OnDeviceRemoved:        syscall.NewCallback(ncOnDeviceAddedRemoved),
	OnDefaultDeviceChanged: syscall.NewCallback(ncOnDefaultDeviceChanged),
	OnPropertyValueChanged: syscall.NewCallback(ncOnPropertyValueChanged),
}}

// notifyEnum is the enumerator client is registered with, kept alive while
// registered. Only touched on the Worker's COM thread.
var notifyEnum *wca.IMMDeviceEnumerator

func signalChange() {
	select {
	case deviceChanges <- struct{}{}:
	default:
	}
}

func ncQueryInterface(this, riid, ppv uintptr) uintptr {
	iid := *(**ole.GUID)(unsafe.Pointer(&riid))
	out := *(**uintptr)(unsafe.Pointer(&ppv))
	if ole.IsEqualGUID(iid, ole.IID_IUnknown) || ole.IsEqualGUID(iid, wca.IID_IMMNotificationClient) {
		*out = this
		return ole.S_OK
	}
	*out = 0
	return ole.E_NOINTERFACE
}

func ncAddRefRelease(_ uintptr) uintptr { return 1 }

func ncOnDeviceStateChanged(_, _, _ uintptr) uintptr {
	signalChange()
	return ole.S_OK
}

func ncOnDeviceAddedRemoved(_, _ uintptr) uintptr {
	signalChange()
	return ole.S_OK
}

func ncOnDefaultDeviceChanged(_, flow, _, _ uintptr) uintptr {
	if flow == uintptr(wca.ERender) {
		signalChange()
	}
	return ole.S_OK
}

// Property changes (volume, format, ...) fire constantly and never affect
// the device list, so they're ignored.
func ncOnPropertyValueChanged(_, _, _ uintptr) uintptr { return ole.S_OK }

// WatchChanges subscribes to Windows' device notifications and returns a
// channel that receives a (coalesced) signal whenever an output device is
// added, removed, enabled or disabled, or the default output changes.
func (w *Worker) WatchChanges() (<-chan struct{}, error) {
	var err error
	w.do(func() { err = startWatching() })
	if err != nil {
		return nil, err
	}
	return deviceChanges, nil
}

func startWatching() error {
	if notifyEnum != nil {
		return nil
	}
	mmde, err := newEnumerator()
	if err != nil {
		return err
	}
	hr, _, _ := syscall.SyscallN(mmde.VTable().RegisterEndpointNotificationCallback,
		uintptr(unsafe.Pointer(mmde)), uintptr(unsafe.Pointer(client)))
	if hr != 0 {
		mmde.Release()
		return ole.NewError(hr)
	}
	notifyEnum = mmde
	return nil
}

func stopWatching() {
	if notifyEnum == nil {
		return
	}
	syscall.SyscallN(notifyEnum.VTable().UnregisterEndpointNotificationCallback,
		uintptr(unsafe.Pointer(notifyEnum)), uintptr(unsafe.Pointer(client)))
	notifyEnum.Release()
	notifyEnum = nil
}
