//go:build windows

// Package audio wraps the pieces of the Windows Core Audio API needed to
// list playback devices, change which one is the system default, and hear
// about device changes.
package audio

import "fmt"

// Device describes one playback (render) endpoint.
type Device struct {
	ID   string
	Name string
}

func toDevice(dev *iMMDevice) (Device, error) {
	id, err := dev.id()
	if err != nil {
		return Device{}, fmt.Errorf("read device id: %w", err)
	}
	name, err := dev.friendlyName()
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

	collection, err := mmde.enumAudioEndpoints(eRender, deviceStateActive)
	if err != nil {
		return nil, fmt.Errorf("enumerate endpoints: %w", err)
	}
	defer collection.Release()

	count, err := collection.count()
	if err != nil {
		return nil, fmt.Errorf("count endpoints: %w", err)
	}

	devices := make([]Device, 0, count)
	for i := uint32(0); i < count; i++ {
		dev, err := collection.item(i)
		if err != nil {
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

	dev, err := mmde.defaultAudioEndpoint(eRender, eConsole)
	if err != nil {
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
	policyConfig, err := newPolicyConfig()
	if err != nil {
		return fmt.Errorf("create policy config: %w", err)
	}
	defer policyConfig.Release()

	for _, role := range []uint32{eConsole, eMultimedia, eCommunications} {
		if err := policyConfig.setDefaultEndpoint(id, role); err != nil {
			return fmt.Errorf("set default endpoint: %w", err)
		}
	}
	return nil
}
