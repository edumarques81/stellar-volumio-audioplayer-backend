package upnp

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"
)

// extractSOAPAction extracts the action name from the SOAPACTION header.
// Header format: "urn:schemas-upnp-org:service:AVTransport:1#Play"
func extractSOAPAction(r *http.Request) string {
	header := r.Header.Get("SOAPACTION")
	header = strings.Trim(header, "\"")
	if idx := strings.LastIndex(header, "#"); idx >= 0 {
		return header[idx+1:]
	}
	return header
}

// soapWrap wraps an action response body in a SOAP envelope.
func soapWrap(action, serviceType, innerXML string) string {
	return `<?xml version="1.0"?>` +
		`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" ` +
		`s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">` +
		`<s:Body>` +
		`<u:` + action + ` xmlns:u="` + serviceType + `">` +
		innerXML +
		`</u:` + action + `>` +
		`</s:Body></s:Envelope>`
}

// writeSOAPResponse writes a SOAP XML response or a 500 error.
func (h *SOAPHandler) writeSOAPResponse(w http.ResponseWriter, body string, err error) {
	if err != nil {
		log.Error().Err(err).Msg("SOAP action error")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.Write([]byte(body))
}

// extractXMLElement extracts the text content of a named XML element from raw XML.
// This is intentionally simple string parsing to avoid a full XML decode of the SOAP body.
func extractXMLElement(raw, element string) string {
	open := "<" + element
	idx := strings.Index(raw, open)
	if idx < 0 {
		return ""
	}
	// Find the end of the opening tag (handles attributes)
	rest := raw[idx+len(open):]
	closeAngle := strings.Index(rest, ">")
	if closeAngle < 0 {
		return ""
	}
	// Check for self-closing tag
	if closeAngle > 0 && rest[closeAngle-1] == '/' {
		return ""
	}
	valueStart := rest[closeAngle+1:]
	closeTag := "</" + element + ">"
	endIdx := strings.Index(valueStart, closeTag)
	if endIdx < 0 {
		return ""
	}
	return valueStart[:endIdx]
}

// decodeXMLEntities decodes common XML entities back to their characters.
func decodeXMLEntities(s string) string {
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&quot;", `"`)
	s = strings.ReplaceAll(s, "&apos;", "'")
	return s
}

// xmlEscape escapes special XML characters in a string.
func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}

// parseTimeToSeconds converts "h:mm:ss" or "h:mm:ss.fff" to total seconds.
func parseTimeToSeconds(timeStr string) int {
	timeStr = strings.TrimSpace(timeStr)
	if timeStr == "" {
		return 0
	}
	// Strip fractional seconds
	if dot := strings.Index(timeStr, "."); dot >= 0 {
		timeStr = timeStr[:dot]
	}
	parts := strings.Split(timeStr, ":")
	total := 0
	switch len(parts) {
	case 3: // h:mm:ss
		total += atoi(parts[0]) * 3600
		total += atoi(parts[1]) * 60
		total += atoi(parts[2])
	case 2: // mm:ss
		total += atoi(parts[0]) * 60
		total += atoi(parts[1])
	case 1: // ss
		total += atoi(parts[0])
	}
	return total
}

// formatSeconds formats total seconds as "h:mm:ss".
func formatSeconds(totalSec int) string {
	if totalSec < 0 {
		totalSec = 0
	}
	h := totalSec / 3600
	m := (totalSec % 3600) / 60
	s := totalSec % 60
	return fmt.Sprintf("%d:%02d:%02d", h, m, s)
}

// atoi is a helper that returns 0 on parse failure.
func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
}
