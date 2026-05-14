package netinfo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// readFixture loads testdata/<name> and returns it as a Status. Fails the
// test if the file is missing or malformed.
func readFixture(t *testing.T, name string) Status {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %q: %v", name, err)
	}
	var s Status
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("unmarshal fixture %q: %v", name, err)
	}
	return s
}

// TestFixturesLoad asserts the captured pre-state REST fixture loads cleanly
// and has a non-empty Type field. Commit 3b's dedup byte-equality test
// (TestCanonicalMatchesRESTFixture) runs against this same fixture from the
// Pi via STELLAR_NETINFO_FIXTURE_TEST=1.
//
// NOTE: there is intentionally no push_pre.json. The legacy
// Server.BroadcastNetworkStatus emits GetNetworkStatus() which marshals the
// same NetworkStatus struct that the REST /api/v1/network handler returns —
// push-fixture equality is implied by REST-fixture equality through the
// shared struct + json tags.
func TestFixturesLoad(t *testing.T) {
	rest := readFixture(t, "rest_pre.json")
	if rest.Type == "" {
		t.Error("rest fixture has empty Type")
	}
	if rest.IP == "" {
		t.Error("rest fixture has empty IP")
	}
}

// TestCanonicalMatchesRESTFixture asserts that the canonical Reporter on
// the current host produces a Status that, when marshalled, byte-equals
// the pre-change REST fixture captured in Commit 3a. This is the gate
// for safely deleting netinfo_legacy_linux.go.
//
// Meaningful only when run on the same Pi host (in the same network
// state) whose state was captured into rest_pre.json. Gated by env var
// so the rest of the suite isn't host-specific.
//
// CAVEAT: the captured fixture is ethernet state. The known drift between
// canonical and legacy lives in the wifi-quality scaling branch order
// (legacy passes q through directly for q ∈ [0,100]; canonical rescales
// q ∈ [0,70] to q*100/70). For ethernet hosts the wifi codepath is not
// exercised and byte-equality holds. For wifi hosts at low signal the
// dedup is a known behaviour change of ±1 Strength bucket — accepted per
// spec decision #7.
func TestCanonicalMatchesRESTFixture(t *testing.T) {
	if os.Getenv("STELLAR_NETINFO_FIXTURE_TEST") != "1" {
		t.Skip("set STELLAR_NETINFO_FIXTURE_TEST=1 to run on the captured host")
	}
	want := readFixture(t, "rest_pre.json")
	got := NewPlatform().GetStatus()
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("canonical vs fixture mismatch:\ngot:  %s\nwant: %s", gotJSON, wantJSON)
	}
}
