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
