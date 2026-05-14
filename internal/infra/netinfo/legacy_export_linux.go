//go:build linux

package netinfo

// LegacyStatusForREST is a temporary linux-only export of legacyGetStatus.
// During Commit Group 3a, /api/v1/network routes through this helper so
// REST behaviour is bit-identical to the pre-refactor main.go impl —
// preserves the wifi-quality scaling branch order that differed from the
// canonical socketio impl (now netinfo_linux.go). Removed in Commit 3b
// when canonical proves byte-equivalent against testdata/rest_pre.json.
//
// DO NOT add new callers.
func LegacyStatusForREST() Status { return legacyGetStatus() }
