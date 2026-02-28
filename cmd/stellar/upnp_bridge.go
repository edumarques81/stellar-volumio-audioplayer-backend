package main

import (
	"strconv"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/player"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/upnp"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/mpd"
	"github.com/rs/zerolog/log"
)

// upnpBridge adapts the existing player.Service and mpd.Client to the
// upnp.PlayerController interface.
type upnpBridge struct {
	player *player.Service
	mpd    *mpd.Client
}

func (b *upnpBridge) LoadAndPlay(uri string, meta upnp.TrackMetadata) error {
	log.Info().
		Str("uri", uri).
		Str("title", meta.Title).
		Str("artist", meta.Artist).
		Msg("UPnP LoadAndPlay: clearing queue, adding URI, playing")

	// Clear queue, add the stream URI, and play
	if err := b.mpd.Clear(); err != nil {
		return err
	}
	if err := b.mpd.Add(uri); err != nil {
		return err
	}
	return b.mpd.Play(0)
}

func (b *upnpBridge) Play() error {
	return b.player.Play(-1)
}

func (b *upnpBridge) Pause() error {
	return b.player.Pause()
}

func (b *upnpBridge) Stop() error {
	return b.player.Stop()
}

func (b *upnpBridge) Seek(seconds int) error {
	return b.player.Seek(seconds)
}

func (b *upnpBridge) GetPosition() (currentSec int, durationSec int, state upnp.TransportState) {
	status, err := b.mpd.Status()
	if err != nil {
		return 0, 0, upnp.StateStopped
	}

	// Parse elapsed time
	if elapsed, err := strconv.ParseFloat(status["elapsed"], 64); err == nil {
		currentSec = int(elapsed)
	}

	// Parse duration
	if dur, err := strconv.ParseFloat(status["duration"], 64); err == nil {
		durationSec = int(dur)
	}

	// Map MPD state to UPnP transport state
	switch status["state"] {
	case "play":
		state = upnp.StatePlaying
	case "pause":
		state = upnp.StatePaused
	default:
		state = upnp.StateStopped
	}

	return
}
