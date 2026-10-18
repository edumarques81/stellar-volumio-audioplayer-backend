package cast

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestDialPerformsTheConnectionHandshake(t *testing.T) {
	dev, client := newFakeDevice(t)

	// Without a CONNECT on the connection namespace the platform receiver
	// ignores everything that follows, including the GET_STATUS Dial sends.
	connects := dev.sent(nsConnection)
	if len(connects) == 0 {
		t.Fatal("Dial sent no CONNECT")
	}
	if connects[0].DestinationID != platformReceiverID {
		t.Errorf("first CONNECT addressed %q, want %q", connects[0].DestinationID, platformReceiverID)
	}

	// Dial reads the initial status, so the device's volume is known before
	// any command is issued.
	if got := client.Status().Volume; got != 0.4 {
		t.Errorf("Volume = %v, want 0.4 from the device's own status", got)
	}
}

func TestLaunchConnectsToTheAppTransport(t *testing.T) {
	// The media namespace is addressed to the app's transportId, and the app
	// ignores it until a CONNECT arrives there too. The fake device fails the
	// test if a media command precedes that CONNECT.
	dev, client := newFakeDevice(t)

	if err := client.LaunchDefaultMediaReceiver(); err != nil {
		t.Fatalf("LaunchDefaultMediaReceiver: %v", err)
	}

	s := client.Status()
	if s.AppID != DefaultMediaReceiverAppID {
		t.Errorf("AppID = %q, want %q", s.AppID, DefaultMediaReceiverAppID)
	}
	if s.TransportID != "transport-7" {
		t.Errorf("TransportID = %q, want the device's own value", s.TransportID)
	}

	if !dev.waitForConnect("transport-7") {
		t.Error("no CONNECT sent to the app transport")
	}
}

func TestLaunchIsSkippedWhenTheAppIsAlreadyRunning(t *testing.T) {
	dev, client := newFakeDevice(t, func(d *fakeDevice) {
		d.appID = DefaultMediaReceiverAppID
		d.sessionID = "session-existing"
	})

	if err := client.LaunchDefaultMediaReceiver(); err != nil {
		t.Fatalf("LaunchDefaultMediaReceiver: %v", err)
	}

	for _, f := range dev.sent(nsReceiver) {
		var env envelope
		if err := json.Unmarshal([]byte(f.Payload), &env); err == nil && env.Type == typeLaunch {
			t.Error("relaunched an app that was already running, which interrupts playback")
		}
	}
}

func TestLoadSendsMusicTrackMetadata(t *testing.T) {
	// This is the whole reason the client is hand-rolled: the receiver renders
	// album name, track number and album artist only from
	// MusicTrackMediaMetadata, and a generic metadata block drops them.
	dev, client := newFakeDevice(t)

	err := client.Load(Media{
		URL:         "http://192.168.86.25:3000/stream/USB/Kind%20of%20Blue/01.flac",
		ContentType: "audio/flac",
		Duration:    561.6,
		Metadata: &Metadata{
			MetadataType: metadataTypeMusicTrack,
			Title:        "So What",
			AlbumName:    "Kind of Blue",
			Artist:       "Miles Davis",
			AlbumArtist:  "Miles Davis",
			TrackNumber:  1,
			Images:       []Image{{URL: "http://192.168.86.25:3000/albumart?path=USB%2FKind+of+Blue%2F01.flac"}},
		},
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	load, ok := dev.lastLoad()
	if !ok {
		t.Fatal("device received no LOAD")
	}
	if load.Media.ContentType != "audio/flac" {
		t.Errorf("contentType = %q", load.Media.ContentType)
	}
	if load.Media.StreamType != "BUFFERED" {
		t.Errorf("streamType = %q, want BUFFERED", load.Media.StreamType)
	}
	if !load.Autoplay {
		t.Error("autoplay = false; a cast request is expected to start playing")
	}

	md := load.Media.Metadata
	if md == nil {
		t.Fatal("LOAD carried no metadata")
	}
	if md.MetadataType != metadataTypeMusicTrack {
		t.Errorf("metadataType = %d, want %d", md.MetadataType, metadataTypeMusicTrack)
	}
	if md.AlbumName != "Kind of Blue" || md.AlbumArtist != "Miles Davis" || md.TrackNumber != 1 {
		t.Errorf("metadata lost fields: %+v", md)
	}
	if len(md.Images) != 1 || !strings.Contains(md.Images[0].URL, "/albumart") {
		t.Errorf("artwork not sent: %+v", md.Images)
	}
}

func TestLoadMetadataJSONUsesTheReceiversFieldNames(t *testing.T) {
	// A misspelled metadata key is dropped silently by the receiver rather
	// than rejected, so the wire names are pinned here.
	body, err := json.Marshal(&Metadata{
		MetadataType: metadataTypeMusicTrack,
		Title:        "So What",
		AlbumName:    "Kind of Blue",
		AlbumArtist:  "Miles Davis",
		TrackNumber:  1,
		DiscNumber:   1,
		ReleaseDate:  "1959-08-17",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	for _, want := range []string{
		`"metadataType":3`,
		`"albumName":"Kind of Blue"`,
		`"albumArtist":"Miles Davis"`,
		`"trackNumber":1`,
		`"discNumber":1`,
		`"releaseDate":"1959-08-17"`,
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("metadata JSON missing %s:\n%s", want, body)
		}
	}
}

func TestLoadSurfacesLoadFailed(t *testing.T) {
	// A format the receiver refuses comes back as LOAD_FAILED on the same
	// requestId. Without handling it the caller waits out the whole request
	// timeout and reports a timeout instead of an unsupported format.
	_, client := newFakeDevice(t, func(d *fakeDevice) { d.loadVerdict = "LOAD_FAILED" })

	err := client.Load(Media{URL: "http://pi:3000/stream/x.dsf", ContentType: "audio/x-dsf"})
	if err == nil {
		t.Fatal("Load accepted a refused format")
	}
	if !strings.Contains(err.Error(), typeLoadFailed) || !strings.Contains(err.Error(), "UNSUPPORTED_MEDIA") {
		t.Errorf("err = %v, want the device's own reason", err)
	}
	if errors.Is(err, ErrTimeout) {
		t.Error("a refused LOAD was reported as a timeout")
	}
}

func TestLoadRejectsAnAcknowledgedLoadWithNoSession(t *testing.T) {
	// Some refusals arrive as a MEDIA_STATUS with an empty status array rather
	// than a LOAD_FAILED. Reporting success there leaves the UI showing a
	// track the device never played.
	_, client := newFakeDevice(t, func(d *fakeDevice) { d.loadVerdict = "empty" })

	if err := client.Load(Media{URL: "http://pi:3000/stream/x.flac", ContentType: "audio/flac"}); err == nil {
		t.Fatal("Load reported success with no media session")
	}
}

func TestTransportCommandsTrackTheDevicesOwnState(t *testing.T) {
	_, client := newFakeDevice(t)

	if err := client.Load(Media{URL: "http://pi:3000/stream/a.flac", ContentType: "audio/flac"}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := client.Status().PlayerState; got != "PLAYING" {
		t.Fatalf("after Load PlayerState = %q, want PLAYING", got)
	}

	if err := client.Pause(); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if got := client.Status().PlayerState; got != "PAUSED" {
		t.Errorf("after Pause PlayerState = %q, want PAUSED", got)
	}

	if err := client.Play(); err != nil {
		t.Fatalf("Play: %v", err)
	}
	if got := client.Status().PlayerState; got != "PLAYING" {
		t.Errorf("after Play PlayerState = %q, want PLAYING", got)
	}

	if err := client.Seek(90); err != nil {
		t.Fatalf("Seek: %v", err)
	}
	if got := client.Status().CurrentTime; got != 90 {
		t.Errorf("after Seek CurrentTime = %v, want 90", got)
	}
}

func TestTransportCommandsNeedASession(t *testing.T) {
	_, client := newFakeDevice(t)

	tests := []struct {
		name string
		call func() error
		want error
	}{
		{"play with no app", client.Play, ErrNoApp},
		{"pause with no app", client.Pause, ErrNoApp},
		{"stop with no app", client.StopMedia, ErrNoApp},
		{"seek with no app", func() error { return client.Seek(10) }, ErrNoApp},
		{"media status with no app", client.RefreshMediaStatus, ErrNoApp},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestPlayNeedsAMediaSessionEvenWithAnAppRunning(t *testing.T) {
	_, client := newFakeDevice(t, func(d *fakeDevice) {
		d.appID = DefaultMediaReceiverAppID
		d.sessionID = "session-1"
	})

	if err := client.Play(); !errors.Is(err, ErrNoMediaSession) {
		t.Errorf("err = %v, want ErrNoMediaSession", err)
	}
}

func TestStopMediaClearsTheMediaSnapshot(t *testing.T) {
	_, client := newFakeDevice(t)
	if err := client.Load(Media{URL: "http://pi:3000/stream/a.flac", ContentType: "audio/flac"}); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if err := client.StopMedia(); err != nil {
		t.Fatalf("StopMedia: %v", err)
	}

	s := client.Status()
	if s.HasMedia || s.ContentID != "" || s.MediaSessionID != 0 {
		t.Errorf("media state survived STOP: %+v", s)
	}
	// The reason survives on purpose: a caller needs to tell a stop apart from
	// a track that played out, and only the idleReason carries that.
	if s.IdleReason != "CANCELLED" {
		t.Errorf("IdleReason = %q, want CANCELLED", s.IdleReason)
	}
	if s.IdleFinished() {
		t.Error("a cancelled session reported itself as finished")
	}
}

func TestTerminalIdleEndsTheSession(t *testing.T) {
	// The bug this guards: a real Nest Hub answers STOP with the same
	// mediaSessionId and IDLE/CANCELLED rather than an empty status array, so
	// keying session-end on the empty array alone leaves HasMedia true and
	// sends the next command to a session the device has discarded.
	tests := []struct {
		name         string
		state        string
		reason       string
		wantHasMedia bool
		wantFinished bool
	}{
		{"stopped", "IDLE", "CANCELLED", false, false},
		{"played out", "IDLE", "FINISHED", false, true},
		{"replaced by another LOAD", "IDLE", "INTERRUPTED", false, false},
		{"receiver error", "IDLE", "ERROR", false, false},
		// IDLE with no reason is the pre-roll between LOAD and the first
		// buffer. The session is real and about to start.
		{"pre-roll after LOAD", "IDLE", "", true, false},
		{"playing", "PLAYING", "", true, false},
		{"paused", "PAUSED", "", true, false},
		{"buffering", "BUFFERING", "", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, client := newFakeDevice(t)
			if err := client.Load(Media{URL: "http://pi:3000/stream/a.flac", ContentType: "audio/flac"}); err != nil {
				t.Fatalf("Load: %v", err)
			}

			client.applyFrame(frame{
				Namespace: nsMedia,
				Payload: fmt.Sprintf(
					`{"type":"MEDIA_STATUS","status":[{"mediaSessionId":42,"playerState":%q,"idleReason":%q}]}`,
					tt.state, tt.reason),
			})

			s := client.Status()
			if s.HasMedia != tt.wantHasMedia {
				t.Errorf("HasMedia = %v, want %v (%+v)", s.HasMedia, tt.wantHasMedia, s)
			}
			if s.IdleFinished() != tt.wantFinished {
				t.Errorf("IdleFinished = %v, want %v", s.IdleFinished(), tt.wantFinished)
			}
			if !tt.wantHasMedia && s.MediaSessionID != 0 {
				t.Errorf("MediaSessionID = %d, want it cleared", s.MediaSessionID)
			}
		})
	}
}

func TestSetVolumeClampsAndReflectsTheDevice(t *testing.T) {
	tests := []struct {
		name string
		set  float64
		want float64
	}{
		{"mid range", 0.6, 0.6},
		{"below zero clamps", -1, 0},
		{"above one clamps", 4, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, client := newFakeDevice(t)
			if err := client.SetVolume(tt.set); err != nil {
				t.Fatalf("SetVolume: %v", err)
			}
			if got := client.Status().Volume; got != tt.want {
				t.Errorf("Volume = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestQuitAppClearsAppAndMediaState(t *testing.T) {
	// A media session cannot outlive the app that owned it, so quitting must
	// clear both halves — otherwise the UI keeps offering transport controls
	// for a device showing its idle screen.
	_, client := newFakeDevice(t)
	if err := client.Load(Media{URL: "http://pi:3000/stream/a.flac", ContentType: "audio/flac"}); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if err := client.QuitApp(); err != nil {
		t.Fatalf("QuitApp: %v", err)
	}

	s := client.Status()
	if s.AppID != "" || s.SessionID != "" || s.TransportID != "" {
		t.Errorf("app state survived QuitApp: %+v", s)
	}
	if s.HasMedia {
		t.Error("media session survived the app that owned it")
	}
}

func TestQuitAppIsANoOpWithNothingRunning(t *testing.T) {
	_, client := newFakeDevice(t)
	if err := client.QuitApp(); err != nil {
		t.Errorf("QuitApp with no app running: %v", err)
	}
}

func TestUnsolicitedStatusReachesTheHandler(t *testing.T) {
	// Another sender in the same room pressing pause arrives as an
	// unsolicited MEDIA_STATUS with no requestId. It is the only way to learn
	// about it, so it must reach the handler rather than being dropped as an
	// uncorrelated reply.
	updates := make(chan Status, 8)
	devSide, clientSide := net.Pipe()
	dev := &fakeDevice{t: t, net: devSide, transportID: "transport-7", connectedTo: map[string]bool{}}
	go dev.serve()

	client, err := Dial("fake:8009",
		WithDialer(func(string) (net.Conn, error) { return clientSide, nil }),
		WithStatusHandler(func(s Status) { updates <- s }),
	)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	dev.replyWithID(platformReceiverID, nsMedia, 0, mediaStatusPayload{
		Type: typeMediaState,
		Status: []mediaStatus{{
			MediaSessionID: 99,
			PlayerState:    "PAUSED",
			CurrentTime:    12.5,
		}},
	})

	select {
	case s := <-updates:
		if s.PlayerState != "PAUSED" || s.MediaSessionID != 99 {
			t.Errorf("status = %+v", s)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no status update delivered")
	}
}

func TestEmptyMediaStatusArrayEndsTheSession(t *testing.T) {
	// The documented trap: the device signals "nothing is playing" with an
	// empty array, and a client that keeps its last media block shows a track
	// that stopped minutes ago.
	_, client := newFakeDevice(t)
	if err := client.Load(Media{URL: "http://pi:3000/stream/a.flac", ContentType: "audio/flac"}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !client.Status().HasMedia {
		t.Fatal("no media session after Load")
	}

	client.applyFrame(frame{
		Namespace: nsMedia,
		Payload:   `{"type":"MEDIA_STATUS","status":[]}`,
	})

	if s := client.Status(); s.HasMedia || s.ContentID != "" {
		t.Errorf("empty status array left stale media: %+v", s)
	}
}

func TestMediaStatusWithoutAMediaBlockKeepsTheKnownTrack(t *testing.T) {
	// A transport-only MEDIA_STATUS omits the media block entirely. Treating
	// that as "the track is gone" would blank the UI on every pause.
	_, client := newFakeDevice(t)
	if err := client.Load(Media{
		URL:         "http://pi:3000/stream/a.flac",
		ContentType: "audio/flac",
		Duration:    300,
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	before := client.Status()

	client.applyFrame(frame{
		Namespace: nsMedia,
		Payload:   `{"type":"MEDIA_STATUS","status":[{"mediaSessionId":42,"playerState":"PAUSED","currentTime":31.2}]}`,
	})

	after := client.Status()
	if after.PlayerState != "PAUSED" || after.CurrentTime != 31.2 {
		t.Errorf("transport change not applied: %+v", after)
	}
	if after.ContentID != before.ContentID {
		t.Errorf("ContentID = %q, want it kept as %q", after.ContentID, before.ContentID)
	}
}

func TestReceiverStatusWithNoApplicationsClearsEverything(t *testing.T) {
	_, client := newFakeDevice(t)
	if err := client.Load(Media{URL: "http://pi:3000/stream/a.flac", ContentType: "audio/flac"}); err != nil {
		t.Fatalf("Load: %v", err)
	}

	client.applyFrame(frame{
		Namespace: nsReceiver,
		Payload:   `{"type":"RECEIVER_STATUS","status":{"volume":{"level":0.7,"muted":false}}}`,
	})

	s := client.Status()
	if s.AppID != "" || s.TransportID != "" || s.HasMedia {
		t.Errorf("idle receiver left app or media state: %+v", s)
	}
	if s.Volume != 0.7 {
		t.Errorf("Volume = %v, want 0.7", s.Volume)
	}
}

func TestHeartbeatPingIsAnswered(t *testing.T) {
	// A receiver that gets no PONG drops the channel in about ten seconds,
	// which would look like a device that keeps disconnecting itself.
	dev, _ := newFakeDevice(t)

	dev.replyWithID(platformReceiverID, nsHeartbeat, 0, envelope{Type: typePing})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, f := range dev.sent(nsHeartbeat) {
			var env envelope
			if err := json.Unmarshal([]byte(f.Payload), &env); err == nil && env.Type == typePong {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("device PING was never answered with a PONG")
}

func TestClosedFiresWhenTheDeviceGoesAway(t *testing.T) {
	devSide, clientSide := net.Pipe()
	dev := &fakeDevice{t: t, net: devSide, transportID: "transport-7", connectedTo: map[string]bool{}}
	go dev.serve()

	client, err := Dial("fake:8009", WithDialer(func(string) (net.Conn, error) { return clientSide, nil }))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	_ = devSide.Close()

	select {
	case <-client.Closed():
	case <-time.After(2 * time.Second):
		t.Fatal("Closed never fired after the device dropped")
	}

	if err := client.Play(); !errors.Is(err, ErrNoApp) && !errors.Is(err, ErrConnClosed) {
		t.Errorf("err = %v, want a closed-connection or no-app error", err)
	}
}

func TestDialFailureIsReported(t *testing.T) {
	_, err := Dial("nowhere:8009", WithDialer(func(string) (net.Conn, error) {
		return nil, errors.New("no route to host")
	}))
	if err == nil {
		t.Fatal("Dial succeeded against a failing dialer")
	}
	if !strings.Contains(err.Error(), "no route to host") {
		t.Errorf("err = %v, want the dialler's cause", err)
	}
}

func TestStatusPlaying(t *testing.T) {
	tests := []struct {
		state string
		want  bool
	}{
		{"PLAYING", true},
		{"PAUSED", false},
		{"BUFFERING", false},
		{"IDLE", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			if got := (Status{PlayerState: tt.state}).Playing(); got != tt.want {
				t.Errorf("Playing() = %v, want %v", got, tt.want)
			}
		})
	}
}
