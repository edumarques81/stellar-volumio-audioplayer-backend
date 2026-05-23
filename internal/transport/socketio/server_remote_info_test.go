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
