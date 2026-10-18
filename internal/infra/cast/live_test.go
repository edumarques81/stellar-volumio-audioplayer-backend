//go:build castlive

// Package cast's live check. Build-tagged off by default: it needs a real Cast
// device on the LAN and it makes noise.
//
// It has to run on the Pi, not the Mac — NordVPN on the Mac silently blocks
// outbound TCP to port 8009 from non-system binaries and the failure looks like
// "no route to host", which costs an hour if you do not know it.
//
//	GOOS=linux GOARCH=arm64 go test -tags castlive -c ./internal/infra/cast/
//	scp cast.test eduardo@stellar.local:/tmp/
//	ssh eduardo@stellar.local '/tmp/cast.test -test.v -test.run TestLive'
//
// STELLAR_CAST_URL points it at a real stream URL; without one the LOAD half is
// skipped and only discovery and the handshake run.
package cast

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestLiveDiscovery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	d := NewDiscoverer()
	devices, err := d.Refresh(ctx)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if len(devices) == 0 {
		t.Fatal("no Cast devices discovered")
	}
	for _, dev := range devices {
		t.Logf("device: id=%s name=%q model=%q addr=%s group=%v",
			dev.ID, dev.Name, dev.Model, dev.Addr, dev.IsGroup)
	}
}

func TestLiveSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	devices, err := NewDiscoverer().Refresh(ctx)
	if err != nil || len(devices) == 0 {
		t.Fatalf("discovery: %v (%d devices)", err, len(devices))
	}
	target := devices[0]
	t.Logf("target: %q at %s", target.Name, target.Addr)

	updates := make(chan Status, 32)
	client, err := Dial(target.Addr, WithStatusHandler(func(s Status) {
		select {
		case updates <- s:
		default:
		}
	}))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	t.Logf("initial status: %+v", client.Status())

	streamURL := os.Getenv("STELLAR_CAST_URL")
	if streamURL == "" {
		t.Skip("STELLAR_CAST_URL unset; skipping the LOAD half")
	}

	// Leaving a device playing unattended is rude, so the session is always
	// torn down — including on a failure part-way through.
	defer func() {
		if err := client.QuitApp(); err != nil {
			t.Logf("QuitApp: %v", err)
		}
	}()

	err = client.Load(Media{
		URL:         streamURL,
		ContentType: os.Getenv("STELLAR_CAST_TYPE"),
		Metadata: &Metadata{
			MetadataType: metadataTypeMusicTrack,
			Title:        "Live protocol check",
			AlbumName:    "Stellar",
			Artist:       "Stellar",
			AlbumArtist:  "Stellar",
			TrackNumber:  1,
			Images:       []Image{{URL: os.Getenv("STELLAR_CAST_ART")}},
		},
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	t.Logf("after Load: %+v", client.Status())

	time.Sleep(3 * time.Second)
	if err := client.RefreshMediaStatus(); err != nil {
		t.Fatalf("RefreshMediaStatus: %v", err)
	}
	s := client.Status()
	t.Logf("playing: state=%s t=%.1f/%.1f content=%s", s.PlayerState, s.CurrentTime, s.Duration, s.ContentID)
	if !s.Playing() {
		t.Errorf("device is not playing: %+v", s)
	}
	if s.CurrentTime <= 0 {
		t.Errorf("currentTime did not advance: %v", s.CurrentTime)
	}

	if err := client.Pause(); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if got := client.Status().PlayerState; got != "PAUSED" {
		t.Errorf("after Pause state = %q", got)
	}

	if err := client.Seek(60); err != nil {
		t.Fatalf("Seek: %v", err)
	}
	t.Logf("after Seek: %+v", client.Status())

	if err := client.Play(); err != nil {
		t.Fatalf("Play: %v", err)
	}
	time.Sleep(2 * time.Second)
	t.Logf("resumed: %+v", client.Status())

	if err := client.StopMedia(); err != nil {
		t.Fatalf("StopMedia: %v", err)
	}
	if client.Status().HasMedia {
		t.Errorf("media session survived STOP: %+v", client.Status())
	}

	// The read goroutine outlives this function, so the channel is drained
	// rather than closed.
	n := 0
	for {
		select {
		case <-updates:
			n++
			continue
		default:
		}
		break
	}
	t.Logf("%d unsolicited status updates observed", n)
}
