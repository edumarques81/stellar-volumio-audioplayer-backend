package socketio

import (
	"errors"
	"testing"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/device"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/netinfo"
)

// fakeRemoteInfo is a programmable stub for handler-branch tests in later tasks.
type fakeRemoteInfo struct {
	systemInfoFn    func() (SystemInfo, error)
	deviceInfoFn    func() (device.DeviceInfo, error)
	networkStatusFn func() (netinfo.Status, error)
	bitPerfectFn    func() (BitPerfectStatus, error)
	dsdModeFn       func() (DsdModeResponse, error)
	mixerModeFn     func() (MixerModeResponse, error)
}

func (f *fakeRemoteInfo) SystemInfo() (SystemInfo, error)        { return f.systemInfoFn() }
func (f *fakeRemoteInfo) DeviceInfo() (device.DeviceInfo, error) { return f.deviceInfoFn() }
func (f *fakeRemoteInfo) NetworkStatus() (netinfo.Status, error) { return f.networkStatusFn() }
func (f *fakeRemoteInfo) BitPerfect() (BitPerfectStatus, error)  { return f.bitPerfectFn() }
func (f *fakeRemoteInfo) DsdMode() (DsdModeResponse, error)      { return f.dsdModeFn() }
func (f *fakeRemoteInfo) MixerMode() (MixerModeResponse, error)  { return f.mixerModeFn() }

var errStub = errors.New("stub error")

func TestServer_UseRemoteInfo_SetsField(t *testing.T) {
	s := &Server{}
	if s.remoteInfo != nil {
		t.Fatalf("remoteInfo not nil by default")
	}
	stub := &fakeRemoteInfo{}
	s.UseRemoteInfo(stub)
	if s.remoteInfo != stub {
		t.Fatalf("remoteInfo not set")
	}
}

func TestServer_systemInfo_LocalWhenRemoteNil(t *testing.T) {
	s := &Server{}
	got := s.systemInfo()
	// Local impl reads os.Hostname(); just assert Host is non-empty.
	if got.Host == "" {
		t.Errorf("Host = %q, want non-empty (local impl)", got.Host)
	}
}

func TestServer_systemInfo_RemoteSuccess_MergesVersion(t *testing.T) {
	stub := &fakeRemoteInfo{
		systemInfoFn: func() (SystemInfo, error) {
			return SystemInfo{Host: "stellar.local", Hardware: "Raspberry Pi 5"}, nil
		},
	}
	s := &Server{}
	s.UseRemoteInfo(stub)
	got := s.systemInfo()
	if got.Host != "stellar.local" {
		t.Errorf("Host = %q, want stellar.local (from Pi)", got.Host)
	}
	if got.SystemVersion == "" {
		t.Errorf("SystemVersion empty, want Mac binary version merged in")
	}
}

func TestServer_systemInfo_RemoteError_ReturnsZeroValue(t *testing.T) {
	stub := &fakeRemoteInfo{
		systemInfoFn: func() (SystemInfo, error) { return SystemInfo{}, errStub },
	}
	s := &Server{}
	s.UseRemoteInfo(stub)
	got := s.systemInfo()
	if got.Host != "" {
		t.Errorf("Host = %q, want empty on remote error", got.Host)
	}
}

func TestVolumioHandlers_DeviceInfo_RemoteSuccess(t *testing.T) {
	stub := &fakeRemoteInfo{
		deviceInfoFn: func() (device.DeviceInfo, error) {
			return device.DeviceInfo{UUID: "pi-uuid", Name: "stellar.local"}, nil
		},
	}
	s := &Server{}
	s.UseRemoteInfo(stub)
	h := &VolumioHandlers{server: s}
	info, err := h.server.remoteInfo.DeviceInfo()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if info.UUID != "pi-uuid" {
		t.Errorf("UUID = %q, want pi-uuid", info.UUID)
	}
}

func TestServer_networkStatus_RemoteSuccess(t *testing.T) {
	stub := &fakeRemoteInfo{
		networkStatusFn: func() (netinfo.Status, error) {
			return netinfo.Status{Type: "ethernet", IP: "192.168.86.25", Strength: 100}, nil
		},
	}
	s := &Server{}
	s.UseRemoteInfo(stub)
	got := s.networkStatus()
	if got.IP != "192.168.86.25" {
		t.Errorf("IP = %q, want 192.168.86.25", got.IP)
	}
}

func TestServer_networkStatus_RemoteError_ReturnsZeroValue(t *testing.T) {
	stub := &fakeRemoteInfo{
		networkStatusFn: func() (netinfo.Status, error) { return netinfo.Status{}, errStub },
	}
	s := &Server{}
	s.UseRemoteInfo(stub)
	got := s.networkStatus()
	if got.IP != "" {
		t.Errorf("IP = %q, want empty on remote error", got.IP)
	}
}

func TestServer_bitPerfect_RemoteSuccess(t *testing.T) {
	stub := &fakeRemoteInfo{
		bitPerfectFn: func() (BitPerfectStatus, error) {
			return BitPerfectStatus{Status: "ok", Config: []string{"good"}}, nil
		},
	}
	s := &Server{}
	s.UseRemoteInfo(stub)
	got := s.bitPerfect()
	if got.Status != "ok" {
		t.Errorf("Status = %q, want ok", got.Status)
	}
}

func TestServer_bitPerfect_RemoteError_ReturnsZeroValue(t *testing.T) {
	stub := &fakeRemoteInfo{
		bitPerfectFn: func() (BitPerfectStatus, error) { return BitPerfectStatus{}, errStub },
	}
	s := &Server{}
	s.UseRemoteInfo(stub)
	got := s.bitPerfect()
	if got.Status != "" {
		t.Errorf("Status = %q, want empty on remote error", got.Status)
	}
}

func TestServer_dsdMode_RemoteSuccess(t *testing.T) {
	stub := &fakeRemoteInfo{
		dsdModeFn: func() (DsdModeResponse, error) {
			return DsdModeResponse{Mode: "native", Success: true}, nil
		},
	}
	s := &Server{}
	s.UseRemoteInfo(stub)
	got := s.dsdMode()
	if got.Mode != "native" {
		t.Errorf("Mode = %q, want native", got.Mode)
	}
}

func TestServer_dsdMode_RemoteError_ReturnsSafeFallback(t *testing.T) {
	stub := &fakeRemoteInfo{
		dsdModeFn: func() (DsdModeResponse, error) { return DsdModeResponse{}, errStub },
	}
	s := &Server{}
	s.UseRemoteInfo(stub)
	got := s.dsdMode()
	if got.Mode != "native" || got.Success {
		t.Errorf("DsdMode = %+v, want {Mode:native, Success:false}", got)
	}
}

func TestServer_mixerMode_RemoteSuccess(t *testing.T) {
	stub := &fakeRemoteInfo{
		mixerModeFn: func() (MixerModeResponse, error) {
			return MixerModeResponse{Enabled: true, Success: true}, nil
		},
	}
	s := &Server{}
	s.UseRemoteInfo(stub)
	got := s.mixerMode()
	if !got.Enabled {
		t.Errorf("Enabled = false, want true")
	}
}

func TestServer_mixerMode_RemoteError_ReturnsSafeFallback(t *testing.T) {
	stub := &fakeRemoteInfo{
		mixerModeFn: func() (MixerModeResponse, error) { return MixerModeResponse{}, errStub },
	}
	s := &Server{}
	s.UseRemoteInfo(stub)
	got := s.mixerMode()
	if got.Enabled || got.Success {
		t.Errorf("MixerMode = %+v, want {Enabled:false, Success:false}", got)
	}
}
