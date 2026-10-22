package gb28181

import (
	"testing"
	"time"
)

func TestStatusCheckKeepsDeviceBeforeFirstKeepalive(t *testing.T) {
	const id = "status-check-before-keepalive"
	t.Cleanup(func() { Devices.Delete(id) })

	Devices.Store(id, &Device{
		ID:           id,
		RegisterTime: time.Now(),
		UpdateTime:   time.Now(),
		Status:       DeviceRegisterStatus,
	})

	server := &GB28181Server{
		keepaliveInterval: 60,
		HeartbeatInterval: 60 * time.Second,
	}
	server.statusCheck()

	if _, ok := Devices.Load(id); !ok {
		t.Fatal("device was removed before the first keepalive")
	}
}

func TestStatusCheckRemovesDeviceAfterKeepaliveTimeout(t *testing.T) {
	const id = "status-check-keepalive-timeout"
	t.Cleanup(func() { Devices.Delete(id) })

	Devices.Store(id, &Device{
		ID:              id,
		RegisterTime:    time.Now().Add(-time.Hour),
		UpdateTime:      time.Now(),
		LastKeepaliveAt: time.Now().Add(-10 * time.Second),
		Status:          DeviceOnlineStatus,
	})

	server := &GB28181Server{
		keepaliveInterval: 1,
		HeartbeatInterval: time.Hour,
	}
	server.statusCheck()

	if _, ok := Devices.Load(id); ok {
		t.Fatal("device was kept after keepalive timeout")
	}
}

func TestGetDeviceInfosDoesNotMarkOffline(t *testing.T) {
	const id = "get-device-infos-status"
	device := &Device{
		ID:              id,
		RegisterTime:    time.Now(),
		UpdateTime:      time.Now(),
		LastKeepaliveAt: time.Now(),
		Status:          DeviceOnlineStatus,
	}
	Devices.Store(id, device)
	t.Cleanup(func() { Devices.Delete(id) })

	infos := (&GB28181Server{}).getDeviceInfos()
	if device.Status != DeviceOnlineStatus {
		t.Fatalf("status = %s", device.Status)
	}

	found := false
	for _, item := range infos.DeviceItems {
		if item.DeviceId == id {
			found = true
		}
	}
	if !found {
		t.Fatal("device missing from list")
	}
}
