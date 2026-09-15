//go:build windows

package audio

import (
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/go-ole/go-ole"
)

// deviceChanges gets a signal whenever Windows reports an output device
// change. Its buffer of 1 coalesces a burst into one pending signal
// instead of ever blocking the Windows thread that delivers it.
var deviceChanges = make(chan struct{}, 1)

// lastChange is when the most recent change was reported, in UnixNano.
var lastChange atomic.Int64

// notificationClient is a minimal IMMNotificationClient COM object that
// lives for the whole process, so it needs no reference counting. Windows
// calls it on its own threads; every method only records the time and
// does a non-blocking send.
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
var notifyEnum *iMMDeviceEnumerator

func signalChange() {
	lastChange.Store(time.Now().UnixNano())
	select {
	case deviceChanges <- struct{}{}:
	default:
	}
}

// LastChange returns when Windows last reported an output device change
// (the zero time if it hasn't since WatchChanges).
func LastChange() time.Time {
	return time.Unix(0, lastChange.Load())
}

func ncQueryInterface(this, riid, ppv uintptr) uintptr {
	iid := *(**ole.GUID)(unsafe.Pointer(&riid))
	out := *(**uintptr)(unsafe.Pointer(&ppv))
	if ole.IsEqualGUID(iid, ole.IID_IUnknown) || ole.IsEqualGUID(iid, iidIMMNotificationClient) {
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
	if flow == eRender {
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
	if err := mmde.registerNotificationClient(client); err != nil {
		mmde.Release()
		return err
	}
	notifyEnum = mmde
	return nil
}

func stopWatching() {
	if notifyEnum == nil {
		return
	}
	notifyEnum.unregisterNotificationClient(client)
	notifyEnum.Release()
	notifyEnum = nil
}
