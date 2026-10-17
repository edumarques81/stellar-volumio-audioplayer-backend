package main

import (
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/airplay"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/lcd"
	mpdclient "github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/mpd"
)

// displayHardware adapts lcd.Controller to display.Hardware. The display
// package deliberately does not import lcd: its schedules must only be able
// to power the panel, not reach for anything else on it.
type displayHardware struct{ c lcd.Controller }

func (h displayHardware) Set(on bool) error { return h.c.Set(on) }

func (h displayHardware) Status() (bool, error) {
	status, err := h.c.Status()
	return status.IsOn, err
}

// displayPlayback answers "is audio running right now" for the night
// schedule. Both sources count: an AirPlay session is still someone
// listening, and blacking the panel mid-stream would be the same bug as
// doing it mid-album.
type displayPlayback struct {
	mpd     *mpdclient.Client
	airplay *airplay.Session
}

func (p displayPlayback) IsPlaying() bool {
	if p.airplay != nil {
		if snap := p.airplay.Snapshot(); snap.IsActive && snap.IsPlaying {
			return true
		}
	}
	if p.mpd == nil {
		return false
	}
	status, err := p.mpd.Status()
	if err != nil {
		// An unreachable MPD is not evidence that nothing is playing, but it
		// is the only answer available; the idle timer is an hour long, so a
		// transient blip cannot black the screen on its own.
		return false
	}
	return status["state"] == "play"
}
