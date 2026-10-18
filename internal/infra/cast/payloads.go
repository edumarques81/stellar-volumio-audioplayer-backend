package cast

// Cast namespaces. Everything a sender needs lives in these four.
const (
	nsConnection = "urn:x-cast:com.google.cast.tp.connection"
	nsHeartbeat  = "urn:x-cast:com.google.cast.tp.heartbeat"
	nsReceiver   = "urn:x-cast:com.google.cast.receiver"
	nsMedia      = "urn:x-cast:com.google.cast.media"
)

// DefaultMediaReceiverAppID is Google's stock media player — the receiver app
// that can be launched on any Cast device without registering an app of our
// own with Google.
const DefaultMediaReceiverAppID = "CC1AD845"

// Well-known endpoint IDs. The platform receiver always answers on
// "receiver-0"; the launched app answers on the transportId it reports, which
// is why the media namespace cannot be addressed until after a LAUNCH.
const (
	platformReceiverID = "receiver-0"
	defaultSenderID    = "sender-0"
)

// Message types we send or recognise.
const (
	typeConnect       = "CONNECT"
	typeClose         = "CLOSE"
	typePing          = "PING"
	typePong          = "PONG"
	typeGetStatus     = "GET_STATUS"
	typeLaunch        = "LAUNCH"
	typeSetVolume     = "SET_VOLUME"
	typeStop          = "STOP"
	typeLoad          = "LOAD"
	typePlay          = "PLAY"
	typePause         = "PAUSE"
	typeSeek          = "SEEK"
	typeReceiverState = "RECEIVER_STATUS"
	typeMediaState    = "MEDIA_STATUS"
	typeLoadFailed    = "LOAD_FAILED"
	typeLoadCancelled = "LOAD_CANCELLED"
	typeInvalidReq    = "INVALID_REQUEST"
)

// envelope is the shape every Cast payload shares: a type, and a requestId on
// anything that expects a reply. It is decoded first so the read loop can route
// a frame without knowing its full shape.
type envelope struct {
	Type      string `json:"type"`
	RequestID int    `json:"requestId,omitempty"`
}

type connectPayload struct {
	Type   string         `json:"type"`
	Origin map[string]any `json:"origin"`
}

type requestPayload struct {
	Type      string `json:"type"`
	RequestID int    `json:"requestId"`
}

type launchPayload struct {
	Type      string `json:"type"`
	RequestID int    `json:"requestId"`
	AppID     string `json:"appId"`
}

type stopSessionPayload struct {
	Type      string `json:"type"`
	RequestID int    `json:"requestId"`
	SessionID string `json:"sessionId"`
}

type setVolumePayload struct {
	Type      string       `json:"type"`
	RequestID int          `json:"requestId"`
	Volume    volumeTarget `json:"volume"`
}

// volumeTarget sets level and muted independently: a SET_VOLUME carrying both
// at once is accepted, but sending a zero level alongside muted=false is how
// you accidentally silence a device, so the client sets one at a time.
type volumeTarget struct {
	Level *float64 `json:"level,omitempty"`
	Muted *bool    `json:"muted,omitempty"`
}

// --- media namespace ---

type mediaRequestPayload struct {
	Type           string `json:"type"`
	RequestID      int    `json:"requestId"`
	MediaSessionID int    `json:"mediaSessionId"`
}

type seekPayload struct {
	Type           string  `json:"type"`
	RequestID      int     `json:"requestId"`
	MediaSessionID int     `json:"mediaSessionId"`
	CurrentTime    float64 `json:"currentTime"`
	ResumeState    string  `json:"resumeState,omitempty"`
}

type loadPayload struct {
	Type        string         `json:"type"`
	RequestID   int            `json:"requestId"`
	Autoplay    bool           `json:"autoplay"`
	CurrentTime float64        `json:"currentTime"`
	Media       mediaInfo      `json:"media"`
	CustomData  map[string]any `json:"customData,omitempty"`
}

type mediaInfo struct {
	ContentID   string    `json:"contentId"`
	ContentType string    `json:"contentType"`
	StreamType  string    `json:"streamType"`
	Duration    float64   `json:"duration,omitempty"`
	Metadata    *Metadata `json:"metadata,omitempty"`
}

// MetadataTypeMusicTrack selects MusicTrackMediaMetadata, the only metadata
// type whose fields the Nest Hub's music UI actually renders — album name,
// track number and album artist are all absent from the generic type.
const MetadataTypeMusicTrack = 3

// Metadata is MusicTrackMediaMetadata. Field names are fixed by the Cast
// receiver; a misspelling is silently dropped rather than rejected, so changing
// one of these tags means re-testing against a real device.
type Metadata struct {
	MetadataType int     `json:"metadataType"`
	Title        string  `json:"title,omitempty"`
	AlbumName    string  `json:"albumName,omitempty"`
	Artist       string  `json:"artist,omitempty"`
	AlbumArtist  string  `json:"albumArtist,omitempty"`
	Composer     string  `json:"composer,omitempty"`
	TrackNumber  int     `json:"trackNumber,omitempty"`
	DiscNumber   int     `json:"discNumber,omitempty"`
	ReleaseDate  string  `json:"releaseDate,omitempty"`
	Images       []Image `json:"images,omitempty"`
}

// Image is one artwork entry. Width and height are optional and the receiver
// picks a size itself when they are absent, which is what we do — the artwork
// endpoint does not report dimensions without decoding the image.
type Image struct {
	URL    string `json:"url"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

// --- inbound status ---

type receiverStatusPayload struct {
	Type   string `json:"type"`
	Status struct {
		Applications []struct {
			AppID        string `json:"appId"`
			DisplayName  string `json:"displayName"`
			SessionID    string `json:"sessionId"`
			StatusText   string `json:"statusText"`
			TransportID  string `json:"transportId"`
			IsIdleScreen bool   `json:"isIdleScreen"`
		} `json:"applications"`
		Volume struct {
			Level float64 `json:"level"`
			Muted bool    `json:"muted"`
		} `json:"volume"`
	} `json:"status"`
}

type mediaStatusPayload struct {
	Type   string        `json:"type"`
	Status []mediaStatus `json:"status"`
}

type mediaStatus struct {
	MediaSessionID int       `json:"mediaSessionId"`
	PlayerState    string    `json:"playerState"`
	CurrentTime    float64   `json:"currentTime"`
	IdleReason     string    `json:"idleReason"`
	Media          mediaInfo `json:"media"`
	Volume         struct {
		Level float64 `json:"level"`
		Muted bool    `json:"muted"`
	} `json:"volume"`
}

type errorPayload struct {
	Type   string `json:"type"`
	Reason string `json:"reason"`
}
