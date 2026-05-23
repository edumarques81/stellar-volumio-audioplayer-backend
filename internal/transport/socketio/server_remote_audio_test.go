package socketio

import (
	"errors"
	"testing"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/device"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/netinfo"
)

// fakeRemoteAudio is a programmable stub for handler-branch tests in later tasks. Satisfies RemoteAudioClient.
type fakeRemoteAudio struct {
	systemInfoFn      func() (SystemInfo, error)
	deviceInfoFn      func() (device.DeviceInfo, error)
	networkStatusFn   func() (netinfo.Status, error)
	bitPerfectFn      func() (BitPerfectStatus, error)
	dsdModeFn         func() (DsdModeResponse, error)
	mixerModeFn       func() (MixerModeResponse, error)
	setDsdModeFn      func(mode string) (DsdModeResponse, error)
	setMixerModeFn    func(enabled bool) (MixerModeResponse, error)
	applyBitPerfectFn func() (ApplyBitPerfectResponse, error)
}

func (f *fakeRemoteAudio) SystemInfo() (SystemInfo, error)        { return f.systemInfoFn() }
func (f *fakeRemoteAudio) DeviceInfo() (device.DeviceInfo, error) { return f.deviceInfoFn() }
func (f *fakeRemoteAudio) NetworkStatus() (netinfo.Status, error) { return f.networkStatusFn() }
func (f *fakeRemoteAudio) BitPerfect() (BitPerfectStatus, error)  { return f.bitPerfectFn() }
func (f *fakeRemoteAudio) DsdMode() (DsdModeResponse, error)      { return f.dsdModeFn() }
func (f *fakeRemoteAudio) MixerMode() (MixerModeResponse, error)  { return f.mixerModeFn() }
func (f *fakeRemoteAudio) SetDsdMode(mode string) (DsdModeResponse, error) {
	return f.setDsdModeFn(mode)
}
func (f *fakeRemoteAudio) SetMixerMode(enabled bool) (MixerModeResponse, error) {
	return f.setMixerModeFn(enabled)
}
func (f *fakeRemoteAudio) ApplyBitPerfect() (ApplyBitPerfectResponse, error) {
	return f.applyBitPerfectFn()
}

var errStub = errors.New("stub error")

func TestServer_UseRemoteAudio_SetsField(t *testing.T) {
	s := &Server{}
	if s.remoteAudio != nil {
		t.Fatalf("remoteAudio not nil by default")
	}
	stub := &fakeRemoteAudio{}
	s.UseRemoteAudio(stub)
	if s.remoteAudio != stub {
		t.Fatalf("remoteAudio not set")
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
	stub := &fakeRemoteAudio{
		systemInfoFn: func() (SystemInfo, error) {
			return SystemInfo{Host: "stellar.local", Hardware: "Raspberry Pi 5"}, nil
		},
	}
	s := &Server{}
	s.UseRemoteAudio(stub)
	got := s.systemInfo()
	if got.Host != "stellar.local" {
		t.Errorf("Host = %q, want stellar.local (from Pi)", got.Host)
	}
	if got.SystemVersion == "" {
		t.Errorf("SystemVersion empty, want Mac binary version merged in")
	}
}

func TestServer_systemInfo_RemoteError_ReturnsZeroValue(t *testing.T) {
	stub := &fakeRemoteAudio{
		systemInfoFn: func() (SystemInfo, error) { return SystemInfo{}, errStub },
	}
	s := &Server{}
	s.UseRemoteAudio(stub)
	got := s.systemInfo()
	if got.Host != "" {
		t.Errorf("Host = %q, want empty on remote error", got.Host)
	}
}

func TestVolumioHandlers_DeviceInfo_RemoteSuccess(t *testing.T) {
	stub := &fakeRemoteAudio{
		deviceInfoFn: func() (device.DeviceInfo, error) {
			return device.DeviceInfo{UUID: "pi-uuid", Name: "stellar.local"}, nil
		},
	}
	s := &Server{}
	s.UseRemoteAudio(stub)
	h := &VolumioHandlers{server: s}
	info, err := h.server.remoteAudio.DeviceInfo()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if info.UUID != "pi-uuid" {
		t.Errorf("UUID = %q, want pi-uuid", info.UUID)
	}
}

func TestServer_networkStatus_RemoteSuccess(t *testing.T) {
	stub := &fakeRemoteAudio{
		networkStatusFn: func() (netinfo.Status, error) {
			return netinfo.Status{Type: "ethernet", IP: "192.168.86.25", Strength: 100}, nil
		},
	}
	s := &Server{}
	s.UseRemoteAudio(stub)
	got := s.networkStatus()
	if got.IP != "192.168.86.25" {
		t.Errorf("IP = %q, want 192.168.86.25", got.IP)
	}
}

func TestServer_networkStatus_RemoteError_ReturnsZeroValue(t *testing.T) {
	stub := &fakeRemoteAudio{
		networkStatusFn: func() (netinfo.Status, error) { return netinfo.Status{}, errStub },
	}
	s := &Server{}
	s.UseRemoteAudio(stub)
	got := s.networkStatus()
	if got.IP != "" {
		t.Errorf("IP = %q, want empty on remote error", got.IP)
	}
}

func TestServer_bitPerfect_RemoteSuccess(t *testing.T) {
	stub := &fakeRemoteAudio{
		bitPerfectFn: func() (BitPerfectStatus, error) {
			return BitPerfectStatus{Status: "ok", Config: []string{"good"}}, nil
		},
	}
	s := &Server{}
	s.UseRemoteAudio(stub)
	got := s.bitPerfect()
	if got.Status != "ok" {
		t.Errorf("Status = %q, want ok", got.Status)
	}
}

func TestServer_bitPerfect_RemoteError_ReturnsZeroValue(t *testing.T) {
	stub := &fakeRemoteAudio{
		bitPerfectFn: func() (BitPerfectStatus, error) { return BitPerfectStatus{}, errStub },
	}
	s := &Server{}
	s.UseRemoteAudio(stub)
	got := s.bitPerfect()
	if got.Status != "" {
		t.Errorf("Status = %q, want empty on remote error", got.Status)
	}
}

func TestServer_dsdMode_RemoteSuccess(t *testing.T) {
	stub := &fakeRemoteAudio{
		dsdModeFn: func() (DsdModeResponse, error) {
			return DsdModeResponse{Mode: "native", Success: true}, nil
		},
	}
	s := &Server{}
	s.UseRemoteAudio(stub)
	got := s.dsdMode()
	if got.Mode != "native" {
		t.Errorf("Mode = %q, want native", got.Mode)
	}
}

func TestServer_dsdMode_RemoteError_ReturnsSafeFallback(t *testing.T) {
	stub := &fakeRemoteAudio{
		dsdModeFn: func() (DsdModeResponse, error) { return DsdModeResponse{}, errStub },
	}
	s := &Server{}
	s.UseRemoteAudio(stub)
	got := s.dsdMode()
	if got.Mode != "native" || got.Success {
		t.Errorf("DsdMode = %+v, want {Mode:native, Success:false}", got)
	}
}

func TestServer_mixerMode_RemoteSuccess(t *testing.T) {
	stub := &fakeRemoteAudio{
		mixerModeFn: func() (MixerModeResponse, error) {
			return MixerModeResponse{Enabled: true, Success: true}, nil
		},
	}
	s := &Server{}
	s.UseRemoteAudio(stub)
	got := s.mixerMode()
	if !got.Enabled {
		t.Errorf("Enabled = false, want true")
	}
}

func TestServer_mixerMode_RemoteError_ReturnsSafeFallback(t *testing.T) {
	stub := &fakeRemoteAudio{
		mixerModeFn: func() (MixerModeResponse, error) { return MixerModeResponse{}, errStub },
	}
	s := &Server{}
	s.UseRemoteAudio(stub)
	got := s.mixerMode()
	if got.Enabled || got.Success {
		t.Errorf("MixerMode = %+v, want {Enabled:false, Success:false}", got)
	}
}
