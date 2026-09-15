//go:build windows

// Package audio wraps the pieces of the Windows Core Audio API needed to
// list playback devices and change which one is the system default.
package audio

import (
	"fmt"

	"github.com/moutend/go-wca/pkg/wca"
)

// Device describes one playback (render) endpoint.
type Device struct {
	ID   string
	Name string
}

func newEnumerator() (*wca.IMMDeviceEnumerator, error) {
	var mmde *wca.IMMDeviceEnumerator
	if err := wca.CoCreateInstance(wca.CLSID_MMDeviceEnumerator, 0, wca.CLSCTX_ALL, wca.IID_IMMDeviceEnumerator, &mmde); err != nil {
		return nil, fmt.Errorf("create device enumerator: %w", err)
	}
	return mmde, nil
}

func deviceID(dev *wca.IMMDevice) (string, error) {
	var id string
	if err := dev.GetId(&id); err != nil {
		return "", fmt.Errorf("read device id: %w", err)
	}
	return id, nil
}

func friendlyName(dev *wca.IMMDevice) (string, error) {
	var store *wca.IPropertyStore
	if err := dev.OpenPropertyStore(wca.STGM_READ, &store); err != nil {
		return "", fmt.Errorf("open property store: %w", err)
	}
	defer store.Release()

	var pv wca.PROPVARIANT
	if err := store.GetValue(&wca.PKEY_Device_FriendlyName, &pv); err != nil {
		return "", fmt.Errorf("read friendly name: %w", err)
	}
	return pv.String(), nil
}

func toDevice(dev *wca.IMMDevice) (Device, error) {
	id, err := deviceID(dev)
	if err != nil {
		return Device{}, err
	}
	name, err := friendlyName(dev)
	if err != nil {
		// Fall back to the raw endpoint ID rather than failing outright;
		// a device is still usable even if Windows can't name it.
		name = id
	}
	return Device{ID: id, Name: name}, nil
}

// List returns every currently active playback device, in the order
// Windows itself reports them.
func List() ([]Device, error) {
	mmde, err := newEnumerator()
	if err != nil {
		return nil, err
	}
	defer mmde.Release()

	var collection *wca.IMMDeviceCollection
	if err := mmde.EnumAudioEndpoints(wca.ERender, wca.DEVICE_STATE_ACTIVE, &collection); err != nil {
		return nil, fmt.Errorf("enumerate endpoints: %w", err)
	}
	defer collection.Release()

	var count uint32
	if err := collection.GetCount(&count); err != nil {
		return nil, fmt.Errorf("count endpoints: %w", err)
	}

	devices := make([]Device, 0, count)
	for i := uint32(0); i < count; i++ {
		var dev *wca.IMMDevice
		if err := collection.Item(i, &dev); err != nil {
			return nil, fmt.Errorf("read endpoint %d: %w", i, err)
		}
		d, err := toDevice(dev)
		dev.Release()
		if err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, nil
}

// Current returns the device currently used for the "console" role, i.e.
// the one regular desktop applications play sound through.
func Current() (Device, error) {
	mmde, err := newEnumerator()
	if err != nil {
		return Device{}, err
	}
	defer mmde.Release()

	var dev *wca.IMMDevice
	if err := mmde.GetDefaultAudioEndpoint(wca.ERender, wca.EConsole, &dev); err != nil {
		return Device{}, fmt.Errorf("get default endpoint: %w", err)
	}
	defer dev.Release()

	return toDevice(dev)
}

// SetDefault makes the endpoint identified by id the default device for
// all three roles (console, multimedia, communications). Setting all
// three is what makes the switch take effect for every application,
// matching what the Windows sound settings UI does.
func SetDefault(id string) error {
	var policyConfig *iPolicyConfig
	if err := wca.CoCreateInstance(clsidPolicyConfig, 0, wca.CLSCTX_ALL, iidPolicyConfig, &policyConfig); err != nil {
		return fmt.Errorf("create policy config: %w", err)
	}
	defer policyConfig.Release()

	for _, role := range []uint32{wca.EConsole, wca.EMultimedia, wca.ECommunications} {
		if err := policyConfig.setDefaultEndpoint(id, role); err != nil {
			return fmt.Errorf("set default endpoint: %w", err)
		}
	}
	return nil
}
