// Package upnp implements a UPnP AV Renderer so that UPnP Control Points
// (such as Audirvana) can discover Stellar on the LAN and stream audio to it.
package upnp

// DeviceConfig describes the UPnP device we advertise.
type DeviceConfig struct {
	FriendlyName string // Shown in the control point's output list (e.g. "Stellar Audio")
	UDN          string // Unique Device Name — a stable UUID
	Port         int    // HTTP port for UPnP description + SOAP (e.g. 9180)
	LocalIP      string // Auto-detected LAN IP address
}

// TransportState mirrors the UPnP AVTransport state variable.
type TransportState string

const (
	StateStopped       TransportState = "STOPPED"
	StatePlaying       TransportState = "PLAYING"
	StatePaused        TransportState = "PAUSED_PLAYBACK"
	StateTransitioning TransportState = "TRANSITIONING"
	StateNoMedia       TransportState = "NO_MEDIA_PRESENT"
)

// AVTransportInfo is returned by GetTransportInfo.
type AVTransportInfo struct {
	CurrentTransportState  TransportState
	CurrentTransportStatus string // "OK" or "ERROR_OCCURRED"
	CurrentSpeed           string // "1"
}

// PositionInfo is returned by GetPositionInfo.
type PositionInfo struct {
	Track         int
	TrackDuration string // "h:mm:ss"
	TrackURI      string
	RelTime       string // "h:mm:ss" — current playback position
	AbsTime       string // "h:mm:ss"
}

// TrackMetadata holds metadata parsed from DIDL-Lite XML sent by the control point.
type TrackMetadata struct {
	Title    string
	Artist   string
	Album    string
	AlbumArt string
	Duration string // "h:mm:ss"
	URI      string
}

// PlayerController is the interface the UPnP service uses to control playback.
// It is implemented by a bridge to the MPD player service.
type PlayerController interface {
	LoadAndPlay(uri string, metadata TrackMetadata) error
	Play() error
	Pause() error
	Stop() error
	Seek(seconds int) error
	GetPosition() (currentSec int, durationSec int, state TransportState)
}
