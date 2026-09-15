//go:build windows

package audio

import (
	"syscall"
	"unsafe"

	"github.com/go-ole/go-ole"
)

// IPolicyConfig is an undocumented COM interface that Windows itself uses
// to change the default audio endpoint. It is not part of any public SDK
// header, but its GUIDs and vtable layout have been stable since Windows 7
// and are relied upon by many well known audio-switching tools (EarTrumpet,
// NirCmd, AudioSwitch, ...).
var (
	clsidPolicyConfig = ole.NewGUID("{870af99c-171d-4f9e-af0d-e63df40c2bc9}")
	iidPolicyConfig   = ole.NewGUID("{f8679f50-850a-41cf-9c72-430f290290c8}")
)

type iPolicyConfig struct {
	ole.IUnknown
}

type iPolicyConfigVtbl struct {
	ole.IUnknownVtbl
	GetMixFormat          uintptr
	GetDeviceFormat       uintptr
	ResetDeviceFormat     uintptr
	SetDeviceFormat       uintptr
	GetProcessingPeriod   uintptr
	SetProcessingPeriod   uintptr
	GetShareMode          uintptr
	SetShareMode          uintptr
	GetPropertyValue      uintptr
	SetPropertyValue      uintptr
	SetDefaultEndpoint    uintptr
	SetEndpointVisibility uintptr
}

func (v *iPolicyConfig) vtable() *iPolicyConfigVtbl {
	return (*iPolicyConfigVtbl)(unsafe.Pointer(v.RawVTable))
}

// setDefaultEndpoint assigns the endpoint identified by deviceID to the
// given ERole (console, multimedia or communications).
func (v *iPolicyConfig) setDefaultEndpoint(deviceID string, role uint32) error {
	idPtr, err := syscall.UTF16PtrFromString(deviceID)
	if err != nil {
		return err
	}
	hr, _, _ := syscall.SyscallN(
		v.vtable().SetDefaultEndpoint,
		uintptr(unsafe.Pointer(v)),
		uintptr(unsafe.Pointer(idPtr)),
		uintptr(role),
	)
	if hr != 0 {
		return ole.NewError(hr)
	}
	return nil
}
