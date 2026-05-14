//go:build !linux

package netinfo

// LegacyStatusForREST is a cross-platform shim that defers to the
// canonical reporter on non-linux platforms. The legacy impl this shim
// mirrors only ever existed on linux; darwin/windows builds use the
// canonical reporter as both "legacy" and "current". Removed in Commit 3b
// alongside the linux variant.
func LegacyStatusForREST() Status { return NewPlatform().GetStatus() }
