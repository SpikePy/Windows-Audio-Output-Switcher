//go:build windows

package audio

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/go-ole/go-ole"
	"golang.org/x/sys/windows"
)

// Minimal bindings, on top of go-ole, for the few Core Audio (MMDevice
// API) COM interfaces this package uses. Each vtable struct lists its
// methods in the order mmdeviceapi.h / propsys.h declare them.

var (
	clsidMMDeviceEnumerator  = ole.NewGUID("{BCDE0395-E52F-467C-8E3D-C4579291692E}")
	iidIMMDeviceEnumerator   = ole.NewGUID("{A95664D2-9614-4F35-A746-DE8DB63617E6}")
	iidIMMNotificationClient = ole.NewGUID("{7991EEC9-7E89-4D85-8390-6C703CEC60C0}")

	pkeyDeviceFriendlyName = propertyKey{
		fmtID: *ole.NewGUID("{A45C254E-DF1C-4EFD-8020-67D146A850E0}"),
		pid:   14,
	}

	procPropVariantClear = windows.NewLazySystemDLL("ole32.dll").NewProc("PropVariantClear")
)

// EDataFlow, ERole and DEVICE_STATE values from mmdeviceapi.h.
const (
	eRender = 0

	eConsole        = 0
	eMultimedia     = 1
	eCommunications = 2

	deviceStateActive = 1

	stgmRead = 0
	vtLPWSTR = 31
)

type propertyKey struct {
	fmtID ole.GUID
	pid   uint32
}

// propVariant is PROPVARIANT's 64-bit layout: a type tag, padding, and a
// 16-byte value union of which only the first word is read here.
type propVariant struct {
	vt  uint16
	_   [6]byte
	val uintptr
	_   uintptr
}

func hrError(hr uintptr) error {
	if hr != 0 {
		return ole.NewError(hr)
	}
	return nil
}

type iMMDeviceEnumerator struct{ ole.IUnknown }

type iMMDeviceEnumeratorVtbl struct {
	ole.IUnknownVtbl
	EnumAudioEndpoints                     uintptr
	GetDefaultAudioEndpoint                uintptr
	GetDevice                              uintptr
	RegisterEndpointNotificationCallback   uintptr
	UnregisterEndpointNotificationCallback uintptr
}

func newEnumerator() (*iMMDeviceEnumerator, error) {
	unk, err := ole.CreateInstance(clsidMMDeviceEnumerator, iidIMMDeviceEnumerator)
	if err != nil {
		return nil, fmt.Errorf("create device enumerator: %w", err)
	}
	return (*iMMDeviceEnumerator)(unsafe.Pointer(unk)), nil
}

func (e *iMMDeviceEnumerator) vtbl() *iMMDeviceEnumeratorVtbl {
	return (*iMMDeviceEnumeratorVtbl)(unsafe.Pointer(e.RawVTable))
}

func (e *iMMDeviceEnumerator) enumAudioEndpoints(flow, stateMask uint32) (*iMMDeviceCollection, error) {
	var c *iMMDeviceCollection
	hr, _, _ := syscall.SyscallN(e.vtbl().EnumAudioEndpoints,
		uintptr(unsafe.Pointer(e)), uintptr(flow), uintptr(stateMask), uintptr(unsafe.Pointer(&c)))
	if err := hrError(hr); err != nil {
		return nil, err
	}
	return c, nil
}

func (e *iMMDeviceEnumerator) defaultAudioEndpoint(flow, role uint32) (*iMMDevice, error) {
	var d *iMMDevice
	hr, _, _ := syscall.SyscallN(e.vtbl().GetDefaultAudioEndpoint,
		uintptr(unsafe.Pointer(e)), uintptr(flow), uintptr(role), uintptr(unsafe.Pointer(&d)))
	if err := hrError(hr); err != nil {
		return nil, err
	}
	return d, nil
}

func (e *iMMDeviceEnumerator) registerNotificationClient(c *notificationClient) error {
	hr, _, _ := syscall.SyscallN(e.vtbl().RegisterEndpointNotificationCallback,
		uintptr(unsafe.Pointer(e)), uintptr(unsafe.Pointer(c)))
	return hrError(hr)
}

func (e *iMMDeviceEnumerator) unregisterNotificationClient(c *notificationClient) {
	syscall.SyscallN(e.vtbl().UnregisterEndpointNotificationCallback,
		uintptr(unsafe.Pointer(e)), uintptr(unsafe.Pointer(c)))
}

type iMMDeviceCollection struct{ ole.IUnknown }

type iMMDeviceCollectionVtbl struct {
	ole.IUnknownVtbl
	GetCount uintptr
	Item     uintptr
}

func (c *iMMDeviceCollection) vtbl() *iMMDeviceCollectionVtbl {
	return (*iMMDeviceCollectionVtbl)(unsafe.Pointer(c.RawVTable))
}

func (c *iMMDeviceCollection) count() (uint32, error) {
	var n uint32
	hr, _, _ := syscall.SyscallN(c.vtbl().GetCount, uintptr(unsafe.Pointer(c)), uintptr(unsafe.Pointer(&n)))
	return n, hrError(hr)
}

func (c *iMMDeviceCollection) item(i uint32) (*iMMDevice, error) {
	var d *iMMDevice
	hr, _, _ := syscall.SyscallN(c.vtbl().Item, uintptr(unsafe.Pointer(c)), uintptr(i), uintptr(unsafe.Pointer(&d)))
	if err := hrError(hr); err != nil {
		return nil, err
	}
	return d, nil
}

type iMMDevice struct{ ole.IUnknown }

type iMMDeviceVtbl struct {
	ole.IUnknownVtbl
	Activate          uintptr
	OpenPropertyStore uintptr
	GetId             uintptr
	GetState          uintptr
}

func (d *iMMDevice) vtbl() *iMMDeviceVtbl {
	return (*iMMDeviceVtbl)(unsafe.Pointer(d.RawVTable))
}

// id returns the device's endpoint ID string.
func (d *iMMDevice) id() (string, error) {
	var p *uint16
	hr, _, _ := syscall.SyscallN(d.vtbl().GetId, uintptr(unsafe.Pointer(d)), uintptr(unsafe.Pointer(&p)))
	if err := hrError(hr); err != nil {
		return "", err
	}
	id := windows.UTF16PtrToString(p)
	windows.CoTaskMemFree(unsafe.Pointer(p))
	return id, nil
}

// friendlyName returns the device's name as Windows shows it, e.g.
// "Speakers (Realtek(R) Audio)".
func (d *iMMDevice) friendlyName() (string, error) {
	var store *iPropertyStore
	hr, _, _ := syscall.SyscallN(d.vtbl().OpenPropertyStore,
		uintptr(unsafe.Pointer(d)), stgmRead, uintptr(unsafe.Pointer(&store)))
	if err := hrError(hr); err != nil {
		return "", fmt.Errorf("open property store: %w", err)
	}
	defer store.Release()
	return store.stringValue(&pkeyDeviceFriendlyName)
}

type iPropertyStore struct{ ole.IUnknown }

type iPropertyStoreVtbl struct {
	ole.IUnknownVtbl
	GetCount uintptr
	GetAt    uintptr
	GetValue uintptr
	SetValue uintptr
	Commit   uintptr
}

func (s *iPropertyStore) vtbl() *iPropertyStoreVtbl {
	return (*iPropertyStoreVtbl)(unsafe.Pointer(s.RawVTable))
}

func (s *iPropertyStore) stringValue(key *propertyKey) (string, error) {
	var pv propVariant
	hr, _, _ := syscall.SyscallN(s.vtbl().GetValue,
		uintptr(unsafe.Pointer(s)), uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(&pv)))
	if err := hrError(hr); err != nil {
		return "", err
	}

	var value string
	vt := pv.vt
	if vt == vtLPWSTR {
		value = windows.UTF16PtrToString(*(**uint16)(unsafe.Pointer(&pv.val)))
	}
	procPropVariantClear.Call(uintptr(unsafe.Pointer(&pv)))
	if vt != vtLPWSTR {
		return "", fmt.Errorf("property has type %d, not a string", vt)
	}
	return value, nil
}
