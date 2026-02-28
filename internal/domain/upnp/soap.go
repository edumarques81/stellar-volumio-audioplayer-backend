package upnp

import (
	"fmt"
	"io"
	"net/http"

	"github.com/rs/zerolog/log"
)

// SOAPHandler handles SOAP requests for AVTransport, RenderingControl,
// and ConnectionManager control endpoints.
type SOAPHandler struct {
	service *Service
}

// NewSOAPHandler creates a new SOAP handler backed by the given Service.
func NewSOAPHandler(svc *Service) *SOAPHandler {
	return &SOAPHandler{service: svc}
}

// RegisterHandlers registers the SOAP control and event subscription
// endpoints on the given ServeMux.
func (h *SOAPHandler) RegisterHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/upnp/control/AVTransport", h.handleAVTransport)
	mux.HandleFunc("/upnp/control/RenderingControl", h.handleRenderingControl)
	mux.HandleFunc("/upnp/control/ConnectionManager", h.handleConnectionManager)

	// Event subscription stubs — Audirvana may SUBSCRIBE/UNSUBSCRIBE.
	// Return 200 with a fake SID to keep it happy.
	mux.HandleFunc("/upnp/event/AVTransport", h.handleEventSub)
	mux.HandleFunc("/upnp/event/RenderingControl", h.handleEventSub)
	mux.HandleFunc("/upnp/event/ConnectionManager", h.handleEventSub)
}

// handleAVTransport dispatches AVTransport SOAP actions.
func (h *SOAPHandler) handleAVTransport(w http.ResponseWriter, r *http.Request) {
	action := extractSOAPAction(r)
	log.Debug().Str("action", action).Msg("AVTransport SOAP request")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	var resp string
	switch action {
	case "SetAVTransportURI":
		resp, err = h.setAVTransportURI(body)
	case "Play":
		resp, err = h.play()
	case "Pause":
		resp, err = h.pause()
	case "Stop":
		resp, err = h.stop()
	case "Seek":
		resp, err = h.seek(body)
	case "GetTransportInfo":
		resp, err = h.getTransportInfo()
	case "GetPositionInfo":
		resp, err = h.getPositionInfo()
	case "GetCurrentTransportActions":
		resp, err = h.getCurrentTransportActions()
	case "GetMediaInfo":
		resp, err = h.getMediaInfo()
	default:
		log.Warn().Str("action", action).Msg("unknown AVTransport action")
		resp, err = h.emptyResponse(action, "urn:schemas-upnp-org:service:AVTransport:1")
	}

	h.writeSOAPResponse(w, resp, err)
}

// handleRenderingControl dispatches RenderingControl SOAP actions.
func (h *SOAPHandler) handleRenderingControl(w http.ResponseWriter, r *http.Request) {
	action := extractSOAPAction(r)
	log.Debug().Str("action", action).Msg("RenderingControl SOAP request")

	var resp string
	var err error
	switch action {
	case "GetVolume":
		resp = soapWrap("GetVolumeResponse", "urn:schemas-upnp-org:service:RenderingControl:1",
			"<CurrentVolume>100</CurrentVolume>")
	case "GetMute":
		resp = soapWrap("GetMuteResponse", "urn:schemas-upnp-org:service:RenderingControl:1",
			"<CurrentMute>0</CurrentMute>")
	default:
		log.Warn().Str("action", action).Msg("unknown RenderingControl action")
		resp, err = h.emptyResponse(action, "urn:schemas-upnp-org:service:RenderingControl:1")
	}

	h.writeSOAPResponse(w, resp, err)
}

// handleConnectionManager dispatches ConnectionManager SOAP actions.
func (h *SOAPHandler) handleConnectionManager(w http.ResponseWriter, r *http.Request) {
	action := extractSOAPAction(r)
	log.Debug().Str("action", action).Msg("ConnectionManager SOAP request")

	var resp string
	var err error
	switch action {
	case "GetProtocolInfo":
		resp = h.getProtocolInfo()
	case "GetCurrentConnectionIDs":
		resp = soapWrap("GetCurrentConnectionIDsResponse", "urn:schemas-upnp-org:service:ConnectionManager:1",
			"<ConnectionIDs>0</ConnectionIDs>")
	case "GetCurrentConnectionInfo":
		resp = soapWrap("GetCurrentConnectionInfoResponse", "urn:schemas-upnp-org:service:ConnectionManager:1",
			"<RcsID>0</RcsID><AVTransportID>0</AVTransportID>"+
				"<ProtocolInfo></ProtocolInfo><PeerConnectionManager></PeerConnectionManager>"+
				"<PeerConnectionID>-1</PeerConnectionID><Direction>Input</Direction>"+
				"<Status>OK</Status>")
	default:
		log.Warn().Str("action", action).Msg("unknown ConnectionManager action")
		resp, err = h.emptyResponse(action, "urn:schemas-upnp-org:service:ConnectionManager:1")
	}

	h.writeSOAPResponse(w, resp, err)
}

// handleEventSub handles SUBSCRIBE/UNSUBSCRIBE for UPnP eventing.
func (h *SOAPHandler) handleEventSub(w http.ResponseWriter, r *http.Request) {
	sid := "uuid:" + h.service.config.UDN
	w.Header().Set("SID", sid)
	w.Header().Set("TIMEOUT", "Second-300")
	w.WriteHeader(http.StatusOK)
}

// --- AVTransport action implementations ---

func (h *SOAPHandler) setAVTransportURI(body []byte) (string, error) {
	uri := extractXMLElement(string(body), "CurrentURI")
	metaXML := extractXMLElement(string(body), "CurrentURIMetaData")

	// DIDL metadata is typically HTML-entity-encoded inside the SOAP body
	metaXML = decodeXMLEntities(metaXML)

	meta := ParseDIDL(metaXML)
	if meta.URI == "" {
		meta.URI = uri
	}

	log.Info().
		Str("uri", uri).
		Str("title", meta.Title).
		Str("artist", meta.Artist).
		Msg("SetAVTransportURI")

	if err := h.service.controller.LoadAndPlay(uri, meta); err != nil {
		return "", err
	}

	h.service.mu.Lock()
	h.service.state = StateTransitioning
	h.service.current = meta
	h.service.mu.Unlock()

	return soapWrap("SetAVTransportURIResponse", "urn:schemas-upnp-org:service:AVTransport:1", ""), nil
}

func (h *SOAPHandler) play() (string, error) {
	log.Info().Msg("UPnP Play")
	if err := h.service.controller.Play(); err != nil {
		return "", err
	}
	h.service.mu.Lock()
	h.service.state = StatePlaying
	h.service.mu.Unlock()
	return soapWrap("PlayResponse", "urn:schemas-upnp-org:service:AVTransport:1", ""), nil
}

func (h *SOAPHandler) pause() (string, error) {
	log.Info().Msg("UPnP Pause")
	if err := h.service.controller.Pause(); err != nil {
		return "", err
	}
	h.service.mu.Lock()
	h.service.state = StatePaused
	h.service.mu.Unlock()
	return soapWrap("PauseResponse", "urn:schemas-upnp-org:service:AVTransport:1", ""), nil
}

func (h *SOAPHandler) stop() (string, error) {
	log.Info().Msg("UPnP Stop")
	if err := h.service.controller.Stop(); err != nil {
		return "", err
	}
	h.service.mu.Lock()
	h.service.state = StateStopped
	h.service.mu.Unlock()
	return soapWrap("StopResponse", "urn:schemas-upnp-org:service:AVTransport:1", ""), nil
}

func (h *SOAPHandler) seek(body []byte) (string, error) {
	target := extractXMLElement(string(body), "Target")
	seconds := parseTimeToSeconds(target)
	log.Info().Str("target", target).Int("seconds", seconds).Msg("UPnP Seek")

	if err := h.service.controller.Seek(seconds); err != nil {
		return "", err
	}
	return soapWrap("SeekResponse", "urn:schemas-upnp-org:service:AVTransport:1", ""), nil
}

func (h *SOAPHandler) getTransportInfo() (string, error) {
	_, _, state := h.service.controller.GetPosition()

	h.service.mu.Lock()
	h.service.state = state
	h.service.mu.Unlock()

	return soapWrap("GetTransportInfoResponse", "urn:schemas-upnp-org:service:AVTransport:1",
		fmt.Sprintf("<CurrentTransportState>%s</CurrentTransportState>"+
			"<CurrentTransportStatus>OK</CurrentTransportStatus>"+
			"<CurrentSpeed>1</CurrentSpeed>", state)), nil
}

func (h *SOAPHandler) getPositionInfo() (string, error) {
	currentSec, durationSec, _ := h.service.controller.GetPosition()

	h.service.mu.RLock()
	trackURI := h.service.current.URI
	h.service.mu.RUnlock()

	relTime := formatSeconds(currentSec)
	trackDuration := formatSeconds(durationSec)

	return soapWrap("GetPositionInfoResponse", "urn:schemas-upnp-org:service:AVTransport:1",
		fmt.Sprintf("<Track>1</Track>"+
			"<TrackDuration>%s</TrackDuration>"+
			"<TrackURI>%s</TrackURI>"+
			"<RelTime>%s</RelTime>"+
			"<AbsTime>%s</AbsTime>",
			trackDuration, xmlEscape(trackURI), relTime, relTime)), nil
}

func (h *SOAPHandler) getCurrentTransportActions() (string, error) {
	return soapWrap("GetCurrentTransportActionsResponse", "urn:schemas-upnp-org:service:AVTransport:1",
		"<Actions>Play,Stop,Pause,Seek,Next,Previous</Actions>"), nil
}

func (h *SOAPHandler) getMediaInfo() (string, error) {
	h.service.mu.RLock()
	trackURI := h.service.current.URI
	h.service.mu.RUnlock()

	return soapWrap("GetMediaInfoResponse", "urn:schemas-upnp-org:service:AVTransport:1",
		fmt.Sprintf("<NrTracks>1</NrTracks>"+
			"<MediaDuration>0:00:00</MediaDuration>"+
			"<CurrentURI>%s</CurrentURI>"+
			"<CurrentURIMetaData></CurrentURIMetaData>"+
			"<PlayMedium>NETWORK</PlayMedium>",
			xmlEscape(trackURI))), nil
}

func (h *SOAPHandler) getProtocolInfo() string {
	sink := "http-get:*:audio/flac:*," +
		"http-get:*:audio/wav:*," +
		"http-get:*:audio/x-wav:*," +
		"http-get:*:audio/aiff:*," +
		"http-get:*:audio/x-aiff:*," +
		"http-get:*:audio/mpeg:*," +
		"http-get:*:audio/mp4:*," +
		"http-get:*:audio/ogg:*," +
		"http-get:*:audio/x-dsd:*," +
		"http-get:*:audio/L16:*," +
		"http-get:*:audio/L24:*," +
		"http-get:*:application/ogg:*," +
		"http-get:*:*:*"
	return soapWrap("GetProtocolInfoResponse", "urn:schemas-upnp-org:service:ConnectionManager:1",
		"<Source></Source><Sink>"+sink+"</Sink>")
}

func (h *SOAPHandler) emptyResponse(action, serviceType string) (string, error) {
	return soapWrap(action+"Response", serviceType, ""), nil
}
