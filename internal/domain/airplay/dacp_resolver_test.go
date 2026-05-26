package airplay

import "testing"

// TestParseDNSSDResolveOutput verifies the macOS dns-sd parser extracts
// the host + port from the resolve-event stream emitted by
// `dns-sd -L iTunes_Ctrl_<DACP_ID> _dacp._tcp .`
//
// Sample output (whitespace-normalised):
//
//	Lookup iTunes_Ctrl_63B5C32B3D40C25B._dacp._tcp.local
//	 9:11:42.123  iTunes_Ctrl_63B5C32B3D40C25B._dacp._tcp.local. can be reached at iPhone-Eduardo.local.:50556 (interface 4)
func TestParseDNSSDResolveOutput(t *testing.T) {
	out := `Lookup iTunes_Ctrl_63B5C32B3D40C25B._dacp._tcp.local
 9:11:42.123  iTunes_Ctrl_63B5C32B3D40C25B._dacp._tcp.local. can be reached at iPhone-Eduardo.local.:50556 (interface 4)
`
	host, port, ok := ParseDNSSDResolveOutput(out)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if host != "iPhone-Eduardo.local." {
		t.Errorf("host = %q", host)
	}
	if port != 50556 {
		t.Errorf("port = %d, want 50556", port)
	}
}

func TestParseDNSSDResolveOutputEmpty(t *testing.T) {
	_, _, ok := ParseDNSSDResolveOutput("")
	if ok {
		t.Errorf("empty output should return ok=false")
	}
}

// TestParseAvahiResolveOutput covers the Linux fallback path:
// `avahi-resolve -n iTunes_Ctrl_<DACP_ID>._dacp._tcp.local`
// followed by `avahi-browse -r _dacp._tcp --terminate` style emit.
func TestParseAvahiResolveOutput(t *testing.T) {
	out := `=   eth0 IPv4 iTunes_Ctrl_63B5C32B3D40C25B _dacp._tcp local
   hostname = [iphone.local]
   address = [192.168.86.50]
   port = [50556]
   txt = ["DvSv=0x20000" "Ver=131073" "iTSh Version=196622"]
`
	host, port, ok := ParseAvahiResolveOutput(out, "63B5C32B3D40C25B")
	if !ok {
		t.Fatalf("ok=false")
	}
	if host != "192.168.86.50" {
		t.Errorf("host = %q", host)
	}
	if port != 50556 {
		t.Errorf("port = %d", port)
	}
}

func TestParseAvahiResolveOutputFiltersByDACPID(t *testing.T) {
	// Two services in the output; only the matching DACP-ID should win.
	out := `=   eth0 IPv4 iTunes_Ctrl_AAAAAAAAAAAAAAAA _dacp._tcp local
   address = [192.168.86.10]
   port = [10000]
=   eth0 IPv4 iTunes_Ctrl_BBBBBBBBBBBBBBBB _dacp._tcp local
   address = [192.168.86.20]
   port = [20000]
`
	host, port, ok := ParseAvahiResolveOutput(out, "BBBBBBBBBBBBBBBB")
	if !ok {
		t.Fatalf("ok=false")
	}
	if host != "192.168.86.20" || port != 20000 {
		t.Errorf("wrong record: host=%q port=%d", host, port)
	}
}
