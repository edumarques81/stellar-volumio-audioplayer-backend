package upnp

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// upnpRoot mirrors enough of a UPnP device description to assert on it.
type upnpRoot struct {
	XMLName xml.Name `xml:"root"`
	Device  struct {
		DeviceType   string `xml:"deviceType"`
		FriendlyName string `xml:"friendlyName"`
		UDN          string `xml:"UDN"`
		DLNADoc      string `xml:"X_DLNADOC"`
		ServiceList  struct {
			Services []struct {
				ServiceType string `xml:"serviceType"`
				SCPDURL     string `xml:"SCPDURL"`
				ControlURL  string `xml:"controlURL"`
				EventSubURL string `xml:"eventSubURL"`
			} `xml:"service"`
		} `xml:"serviceList"`
	} `xml:"device"`
}

func fetchXML(t *testing.T, mux *http.ServeMux, path string) []byte {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d", path, rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/xml") {
		t.Errorf("GET %s Content-Type = %q, want text/xml", path, ct)
	}
	return rec.Body.Bytes()
}

func TestMediaServerDescription(t *testing.T) {
	mux := http.NewServeMux()
	RegisterMediaServerHandlers(mux, MediaServerConfig{
		FriendlyName: "Stellar Library",
		UDN:          "server-uuid",
		BackendPort:  3000,
	})

	var root upnpRoot
	if err := xml.Unmarshal(fetchXML(t, mux, MediaServerDescPath), &root); err != nil {
		t.Fatalf("device description is not well-formed XML: %v", err)
	}

	if root.Device.DeviceType != "urn:schemas-upnp-org:device:MediaServer:1" {
		t.Errorf("deviceType = %q", root.Device.DeviceType)
	}
	if root.Device.FriendlyName != "Stellar Library" {
		t.Errorf("friendlyName = %q", root.Device.FriendlyName)
	}
	if root.Device.UDN != "uuid:server-uuid" {
		t.Errorf("UDN = %q, want the uuid: prefix", root.Device.UDN)
	}
	if root.Device.DLNADoc != "DMS-1.50" {
		t.Errorf("X_DLNADOC = %q, want DMS-1.50", root.Device.DLNADoc)
	}

	// ContentDirectory is the service that makes this a browsable server; a
	// description without it discovers as a device with nothing to offer.
	var cdFound bool
	for _, svc := range root.Device.ServiceList.Services {
		if svc.ServiceType == "urn:schemas-upnp-org:service:ContentDirectory:1" {
			cdFound = true
			if svc.ControlURL != "/upnp/control/ContentDirectory" {
				t.Errorf("ContentDirectory controlURL = %q", svc.ControlURL)
			}
			if svc.SCPDURL != "/upnp/ContentDirectory.xml" {
				t.Errorf("ContentDirectory SCPDURL = %q", svc.SCPDURL)
			}
		}
	}
	if !cdFound {
		t.Fatalf("description lists no ContentDirectory service: %+v", root.Device.ServiceList.Services)
	}
}

func TestMediaServerDescriptionIsSeparateFromTheRenderer(t *testing.T) {
	// One UPnP root device description declares exactly one deviceType, so the
	// server cannot be folded into the renderer's document. Serving both from
	// the same path would make a client see only whichever it fetched.
	if MediaServerDescPath == "/upnp/description.xml" {
		t.Fatal("MediaServer must not reuse the renderer's description path")
	}
}

func TestContentDirectorySCPDDeclaresBrowse(t *testing.T) {
	mux := http.NewServeMux()
	RegisterMediaServerHandlers(mux, MediaServerConfig{UDN: "x", BackendPort: 3000})

	var scpd struct {
		XMLName    xml.Name `xml:"scpd"`
		ActionList struct {
			Actions []struct {
				Name string `xml:"name"`
			} `xml:"action"`
		} `xml:"actionList"`
	}
	if err := xml.Unmarshal(fetchXML(t, mux, "/upnp/ContentDirectory.xml"), &scpd); err != nil {
		t.Fatalf("SCPD is not well-formed XML: %v", err)
	}

	got := map[string]bool{}
	for _, a := range scpd.ActionList.Actions {
		got[a.Name] = true
	}
	// Browse and GetSystemUpdateID are the two the spec makes mandatory;
	// clients call GetSortCapabilities and GetSearchCapabilities on connect.
	for _, want := range []string{"Browse", "Search", "GetSearchCapabilities", "GetSortCapabilities", "GetSystemUpdateID"} {
		if !got[want] {
			t.Errorf("SCPD does not declare %s", want)
		}
	}
}
