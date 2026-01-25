package socketio_test

import (
	"testing"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/player"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/mpd"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/transport/socketio"
)

func TestNewServer(t *testing.T) {
	// Create mock dependencies
	mpdClient := mpd.NewClient("localhost", 6600, "")
	playerService := player.NewService(mpdClient)

	server, err := socketio.NewServer(playerService, mpdClient, nil, nil, true)
	if err != nil {
		t.Errorf("NewServer should not return error: %v", err)
	}

	if server == nil {
		t.Error("NewServer should return a non-nil server")
	}
}

func TestServerServeHTTP(t *testing.T) {
	// Create mock dependencies
	mpdClient := mpd.NewClient("localhost", 6600, "")
	playerService := player.NewService(mpdClient)

	server, err := socketio.NewServer(playerService, mpdClient, nil, nil, true)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Server should implement http.Handler
	// We verify this by checking it's not nil and can be closed
	if server == nil {
		t.Error("Server should not be nil")
	}

	// Test that Close works
	if err := server.Close(); err != nil {
		t.Errorf("Close should not error: %v", err)
	}
}

func TestServerBroadcastStateWithoutClients(t *testing.T) {
	// Create mock dependencies
	mpdClient := mpd.NewClient("localhost", 6600, "")
	playerService := player.NewService(mpdClient)

	server, err := socketio.NewServer(playerService, mpdClient, nil, nil, true)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	defer server.Close()

	// BroadcastState should not panic with no clients
	// This is a smoke test - it should handle the case gracefully
	server.BroadcastState()
}

func TestServerBroadcastQueueWithoutClients(t *testing.T) {
	// Create mock dependencies
	mpdClient := mpd.NewClient("localhost", 6600, "")
	playerService := player.NewService(mpdClient)

	server, err := socketio.NewServer(playerService, mpdClient, nil, nil, true)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	defer server.Close()

	// BroadcastQueue should not panic with no clients
	server.BroadcastQueue()
}

func TestServerBroadcastNetworkStatusWithoutClients(t *testing.T) {
	// Create mock dependencies
	mpdClient := mpd.NewClient("localhost", 6600, "")
	playerService := player.NewService(mpdClient)

	server, err := socketio.NewServer(playerService, mpdClient, nil, nil, true)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	defer server.Close()

	// BroadcastNetworkStatus should not panic with no clients
	server.BroadcastNetworkStatus()
}

func TestServerBroadcastLCDStatusWithoutClients(t *testing.T) {
	// Create mock dependencies
	mpdClient := mpd.NewClient("localhost", 6600, "")
	playerService := player.NewService(mpdClient)

	server, err := socketio.NewServer(playerService, mpdClient, nil, nil, true)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	defer server.Close()

	// BroadcastLCDStatus should not panic with no clients
	server.BroadcastLCDStatus()
}

func TestGetNetworkStatus(t *testing.T) {
	// GetNetworkStatus should return a valid NetworkStatus struct
	status := socketio.GetNetworkStatus()

	// Type should be one of: wifi, ethernet, none
	validTypes := map[string]bool{"wifi": true, "ethernet": true, "none": true}
	if !validTypes[status.Type] {
		t.Errorf("Invalid network type: %s", status.Type)
	}

	// Strength should be 0-3
	if status.Strength < 0 || status.Strength > 3 {
		t.Errorf("Invalid strength: %d (should be 0-3)", status.Strength)
	}

	// Signal should be 0-100
	if status.Signal < 0 || status.Signal > 100 {
		t.Errorf("Invalid signal: %d (should be 0-100)", status.Signal)
	}
}

func TestGetLCDStatus(t *testing.T) {
	// GetLCDStatus should return a valid LCDStatus struct
	status := socketio.GetLCDStatus()

	// IsOn should be boolean (no additional validation needed, but test doesn't panic)
	t.Logf("LCD status: isOn=%v", status.IsOn)
}

func TestGetBitPerfectStatus(t *testing.T) {
	// GetBitPerfectStatus should return a valid BitPerfectStatus struct
	status := socketio.GetBitPerfectStatus()

	// Status should be one of: ok, warning, error
	validStatuses := map[string]bool{"ok": true, "warning": true, "error": true}
	if !validStatuses[status.Status] {
		t.Errorf("Invalid bit-perfect status: %s (should be ok, warning, or error)", status.Status)
	}

	// Arrays should not be nil (may be empty but not nil)
	if status.Issues == nil {
		t.Error("Issues array should not be nil")
	}
	if status.Warnings == nil {
		t.Error("Warnings array should not be nil")
	}
	if status.Config == nil {
		t.Error("Config array should not be nil")
	}

	t.Logf("Bit-perfect status: %s, issues=%d, warnings=%d, config=%d",
		status.Status, len(status.Issues), len(status.Warnings), len(status.Config))
}

func TestBitPerfectStatusStructure(t *testing.T) {
	// Test that BitPerfectStatus can be properly JSON marshaled
	status := socketio.BitPerfectStatus{
		Status:   "ok",
		Issues:   []string{},
		Warnings: []string{"test warning"},
		Config:   []string{"config1", "config2"},
	}

	if status.Status != "ok" {
		t.Errorf("Expected status 'ok', got '%s'", status.Status)
	}
	if len(status.Issues) != 0 {
		t.Errorf("Expected 0 issues, got %d", len(status.Issues))
	}
	if len(status.Warnings) != 1 {
		t.Errorf("Expected 1 warning, got %d", len(status.Warnings))
	}
	if len(status.Config) != 2 {
		t.Errorf("Expected 2 config items, got %d", len(status.Config))
	}
}

func TestGetPlaybackOptions(t *testing.T) {
	// GetPlaybackOptions should return a valid PlaybackOptionsResponse struct
	response := socketio.GetPlaybackOptions()

	// Options should not be nil
	if response.Options == nil {
		t.Error("Options array should not be nil")
	}

	// SystemCards should not be nil
	if response.SystemCards == nil {
		t.Error("SystemCards array should not be nil")
	}

	// If there are options, check the structure
	if len(response.Options) > 0 {
		outputSection := response.Options[0]

		// First section should be "output"
		if outputSection.ID != "output" {
			t.Errorf("Expected output section ID 'output', got '%s'", outputSection.ID)
		}

		// Should have attributes
		if len(outputSection.Attributes) == 0 {
			t.Error("Output section should have attributes")
		} else {
			// First attribute should be output_device
			attr := outputSection.Attributes[0]
			if attr.Name != "output_device" {
				t.Errorf("Expected attribute name 'output_device', got '%s'", attr.Name)
			}
			if attr.Type != "select" {
				t.Errorf("Expected attribute type 'select', got '%s'", attr.Type)
			}
		}
	}

	t.Logf("Playback options: %d sections, %d system cards", len(response.Options), len(response.SystemCards))
}

func TestPlaybackOptionStructure(t *testing.T) {
	// Test that PlaybackOption can be properly constructed
	option := socketio.PlaybackOption{
		Value: "hdmi0",
		Name:  "HDMI: vc4-hdmi-0",
	}

	if option.Value != "hdmi0" {
		t.Errorf("Expected value 'hdmi0', got '%s'", option.Value)
	}
	if option.Name != "HDMI: vc4-hdmi-0" {
		t.Errorf("Expected name 'HDMI: vc4-hdmi-0', got '%s'", option.Name)
	}
}

func TestPlaybackAttributeStructure(t *testing.T) {
	// Test that PlaybackAttribute can be properly constructed with options
	attr := socketio.PlaybackAttribute{
		Name:  "output_device",
		Type:  "select",
		Value: "usb-audio",
		Options: []socketio.PlaybackOption{
			{Value: "hdmi0", Name: "HDMI: vc4-hdmi-0"},
			{Value: "usb-audio", Name: "USB: Audio Device"},
		},
	}

	if attr.Name != "output_device" {
		t.Errorf("Expected name 'output_device', got '%s'", attr.Name)
	}
	if attr.Type != "select" {
		t.Errorf("Expected type 'select', got '%s'", attr.Type)
	}
	if len(attr.Options) != 2 {
		t.Errorf("Expected 2 options, got %d", len(attr.Options))
	}
}

func TestPlaybackOptionsSectionStructure(t *testing.T) {
	// Test that PlaybackOptionsSection can be properly constructed
	section := socketio.PlaybackOptionsSection{
		ID:   "output",
		Name: "Audio Output",
		Attributes: []socketio.PlaybackAttribute{
			{
				Name:  "output_device",
				Type:  "select",
				Value: "hdmi0",
			},
		},
	}

	if section.ID != "output" {
		t.Errorf("Expected ID 'output', got '%s'", section.ID)
	}
	if section.Name != "Audio Output" {
		t.Errorf("Expected name 'Audio Output', got '%s'", section.Name)
	}
	if len(section.Attributes) != 1 {
		t.Errorf("Expected 1 attribute, got %d", len(section.Attributes))
	}
}

func TestPlaybackOptionsResponseStructure(t *testing.T) {
	// Test the complete response structure
	response := socketio.PlaybackOptionsResponse{
		Options: []socketio.PlaybackOptionsSection{
			{
				ID:   "output",
				Name: "Audio Output",
				Attributes: []socketio.PlaybackAttribute{
					{
						Name:  "output_device",
						Type:  "select",
						Value: "usb-audio",
						Options: []socketio.PlaybackOption{
							{Value: "hdmi0", Name: "HDMI: vc4-hdmi-0"},
							{Value: "usb-audio", Name: "USB: Audio Device"},
						},
					},
				},
			},
		},
		SystemCards: []string{"hdmi0", "usb-audio"},
	}

	if len(response.Options) != 1 {
		t.Errorf("Expected 1 section, got %d", len(response.Options))
	}
	if len(response.SystemCards) != 2 {
		t.Errorf("Expected 2 system cards, got %d", len(response.SystemCards))
	}

	// Verify nested structure
	outputSection := response.Options[0]
	if len(outputSection.Attributes) != 1 {
		t.Errorf("Expected 1 attribute, got %d", len(outputSection.Attributes))
	}
	outputAttr := outputSection.Attributes[0]
	if len(outputAttr.Options) != 2 {
		t.Errorf("Expected 2 options, got %d", len(outputAttr.Options))
	}
}

func TestNormalizeBitPerfectStatus(t *testing.T) {
	// Test that NormalizeBitPerfectStatus correctly maps script values to frontend values
	tests := []struct {
		input    string
		expected string
	}{
		{"bit-perfect", "ok"},
		{"not-bit-perfect", "error"},
		{"warning", "warning"},
		{"ok", "ok"},
		{"error", "error"},
	}

	for _, tc := range tests {
		status := socketio.BitPerfectStatus{
			Status:   tc.input,
			Issues:   []string{},
			Warnings: []string{},
			Config:   []string{},
		}

		normalized := socketio.NormalizeBitPerfectStatus(status)
		if normalized.Status != tc.expected {
			t.Errorf("NormalizeBitPerfectStatus(%q) = %q, want %q", tc.input, normalized.Status, tc.expected)
		}
	}
}

func TestDsdModeResponseStructure(t *testing.T) {
	// Test that DsdModeResponse can be properly constructed
	response := socketio.DsdModeResponse{
		Mode:    "native",
		Success: true,
		Error:   "",
	}

	if response.Mode != "native" {
		t.Errorf("Expected mode 'native', got '%s'", response.Mode)
	}
	if !response.Success {
		t.Error("Expected success to be true")
	}
	if response.Error != "" {
		t.Errorf("Expected empty error, got '%s'", response.Error)
	}

	// Test dop mode
	response2 := socketio.DsdModeResponse{
		Mode:    "dop",
		Success: true,
	}
	if response2.Mode != "dop" {
		t.Errorf("Expected mode 'dop', got '%s'", response2.Mode)
	}
}

func TestGetDsdMode(t *testing.T) {
	// GetDsdMode should return a valid response (even if config doesn't exist)
	response := socketio.GetDsdMode()

	// Mode should be either "native" or "dop"
	validModes := map[string]bool{"native": true, "dop": true}
	if !validModes[response.Mode] {
		t.Errorf("Invalid DSD mode: %s (should be 'native' or 'dop')", response.Mode)
	}

	t.Logf("DSD mode: %s, success: %v", response.Mode, response.Success)
}

// Tests for native Go bit-perfect checker implementation

func TestCheckBitPerfectFromConfig_BitPerfect(t *testing.T) {
	// A bit-perfect MPD config with all settings correct
	mpdConfig := `
audio_output {
	type            "alsa"
	name            "My ALSA Device"
	device          "hw:0,0"
	auto_resample   "no"
	auto_format     "no"
	auto_channels   "no"
	mixer_type      "none"
	dop             "no"
}
`
	alsaConfig := `
pcm.!default {
	type hw
	card 0
}
`
	aplayOutput := `
card 0: U20SU6 [U20 SU6], device 0: USB Audio [USB Audio]
  Subdevices: 1/1
  Subdevice #0: subdevice #0
`

	status := socketio.CheckBitPerfectFromConfig(mpdConfig, alsaConfig, aplayOutput)

	if status.Status != "ok" {
		t.Errorf("Expected status 'ok' for bit-perfect config, got '%s'", status.Status)
		t.Logf("Issues: %v", status.Issues)
		t.Logf("Warnings: %v", status.Warnings)
	}

	if len(status.Issues) > 0 {
		t.Errorf("Expected no issues for bit-perfect config, got: %v", status.Issues)
	}
}

func TestCheckBitPerfectFromConfig_WithResampler(t *testing.T) {
	// Config with resampler enabled - should report issue
	mpdConfig := `
resampler {
	plugin "soxr"
	quality "very high"
}
audio_output {
	type            "alsa"
	name            "My ALSA Device"
	device          "hw:0,0"
}
`
	status := socketio.CheckBitPerfectFromConfig(mpdConfig, "", "")

	if len(status.Issues) == 0 {
		t.Error("Expected issue for resampler config")
	}

	found := false
	for _, issue := range status.Issues {
		if contains(issue, "resampler") || contains(issue, "Resampler") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected resampler issue in: %v", status.Issues)
	}
}

func TestCheckBitPerfectFromConfig_WithVolumeNormalization(t *testing.T) {
	// Config with volume normalization - should report issue
	mpdConfig := `
volume_normalization "yes"
audio_output {
	type            "alsa"
	name            "My ALSA Device"
	device          "hw:0,0"
}
`
	status := socketio.CheckBitPerfectFromConfig(mpdConfig, "", "")

	if len(status.Issues) == 0 {
		t.Error("Expected issue for volume normalization config")
	}

	found := false
	for _, issue := range status.Issues {
		if contains(issue, "normalization") || contains(issue, "Normalization") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected volume normalization issue in: %v", status.Issues)
	}
}

func TestCheckBitPerfectFromConfig_WithAutoResample(t *testing.T) {
	// Config with auto_resample yes - should report issue
	mpdConfig := `
audio_output {
	type            "alsa"
	name            "My ALSA Device"
	device          "hw:0,0"
	auto_resample   "yes"
}
`
	status := socketio.CheckBitPerfectFromConfig(mpdConfig, "", "")

	if len(status.Issues) == 0 {
		t.Error("Expected issue for auto_resample config")
	}

	found := false
	for _, issue := range status.Issues {
		if contains(issue, "auto_resample") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected auto_resample issue in: %v", status.Issues)
	}
}

func TestCheckBitPerfectFromConfig_WithSoftwareMixer(t *testing.T) {
	// Config with software mixer - should report warning
	mpdConfig := `
audio_output {
	type            "alsa"
	name            "My ALSA Device"
	device          "hw:0,0"
	mixer_type      "software"
}
`
	status := socketio.CheckBitPerfectFromConfig(mpdConfig, "", "")

	if len(status.Warnings) == 0 {
		t.Error("Expected warning for software mixer config")
	}

	found := false
	for _, warning := range status.Warnings {
		if contains(warning, "software") || contains(warning, "Mixer") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected mixer warning in: %v", status.Warnings)
	}
}

func TestCheckBitPerfectFromConfig_WithDoP(t *testing.T) {
	// Config with DoP enabled - should report warning
	mpdConfig := `
audio_output {
	type            "alsa"
	name            "My ALSA Device"
	device          "hw:0,0"
	dop             "yes"
}
`
	status := socketio.CheckBitPerfectFromConfig(mpdConfig, "", "")

	if len(status.Warnings) == 0 {
		t.Error("Expected warning for DoP config")
	}

	found := false
	for _, warning := range status.Warnings {
		if contains(warning, "DoP") || contains(warning, "DSD") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected DoP warning in: %v", status.Warnings)
	}
}

func TestCheckBitPerfectFromConfig_WithPlugDevice(t *testing.T) {
	// Config with plug device - should report issue
	mpdConfig := `
audio_output {
	type            "alsa"
	name            "My ALSA Device"
	device          "volumio"
}
`
	status := socketio.CheckBitPerfectFromConfig(mpdConfig, "", "")

	if len(status.Issues) == 0 {
		t.Error("Expected issue for plug device config")
	}
}

func TestCheckBitPerfectFromConfig_ALSAPlugType(t *testing.T) {
	// ALSA config with plug type - should report warning
	mpdConfig := `
audio_output {
	type            "alsa"
	name            "My ALSA Device"
	device          "hw:0,0"
}
`
	alsaConfig := `
pcm.!default {
	type plug
	slave.pcm "hw:0,0"
}
`
	status := socketio.CheckBitPerfectFromConfig(mpdConfig, alsaConfig, "")

	if len(status.Warnings) == 0 {
		t.Error("Expected warning for ALSA plug type")
	}

	found := false
	for _, warning := range status.Warnings {
		if contains(warning, "plug") || contains(warning, "ALSA") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected ALSA plug warning in: %v", status.Warnings)
	}
}

func TestCheckBitPerfectFromConfig_USBDACDetected(t *testing.T) {
	// Test USB DAC detection (Singxer SU-6)
	mpdConfig := `
audio_output {
	type            "alsa"
	device          "hw:0,0"
}
`
	aplayOutput := `
card 0: U20SU6 [U20 SU6], device 0: USB Audio [USB Audio]
  Subdevices: 1/1
`
	status := socketio.CheckBitPerfectFromConfig(mpdConfig, "", aplayOutput)

	found := false
	for _, cfg := range status.Config {
		if contains(cfg, "Singxer") || contains(cfg, "SU-6") || contains(cfg, "U20SU6") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected Singxer SU-6 detection in config: %v", status.Config)
	}
}

func TestCheckBitPerfectFromConfig_NoUSBDAC(t *testing.T) {
	// Test when USB DAC is not detected
	mpdConfig := `
audio_output {
	type            "alsa"
	device          "hw:0,0"
}
`
	aplayOutput := `
card 0: vc4hdmi0 [vc4-hdmi-0], device 0: MAI PCM i2s-hifi-0 [MAI PCM i2s-hifi-0]
  Subdevices: 1/1
`
	status := socketio.CheckBitPerfectFromConfig(mpdConfig, "", aplayOutput)

	found := false
	for _, warning := range status.Warnings {
		if contains(warning, "Singxer") || contains(warning, "SU-6") || contains(warning, "not detected") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected warning about missing Singxer SU-6 in: %v", status.Warnings)
	}
}

func TestCheckBitPerfectFromConfig_StatusDetermination(t *testing.T) {
	tests := []struct {
		name           string
		mpdConfig      string
		expectedStatus string
	}{
		{
			name: "bit-perfect",
			mpdConfig: `
audio_output {
	device          "hw:0,0"
	auto_resample   "no"
	auto_format     "no"
	auto_channels   "no"
	mixer_type      "none"
	dop             "no"
}
`,
			expectedStatus: "ok",
		},
		{
			name: "has issues",
			mpdConfig: `
volume_normalization "yes"
audio_output {
	device          "hw:0,0"
}
`,
			expectedStatus: "error",
		},
		{
			name: "warnings only",
			mpdConfig: `
audio_output {
	device          "hw:0,0"
	mixer_type      "software"
	auto_resample   "no"
	auto_format     "no"
	auto_channels   "no"
}
`,
			expectedStatus: "warning",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status := socketio.CheckBitPerfectFromConfig(tc.mpdConfig, "", "")
			if status.Status != tc.expectedStatus {
				t.Errorf("Expected status '%s', got '%s'", tc.expectedStatus, status.Status)
				t.Logf("Issues: %v", status.Issues)
				t.Logf("Warnings: %v", status.Warnings)
			}
		})
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Tests for MixerMode functionality

func TestMixerModeResponseStructure(t *testing.T) {
	// Test that MixerModeResponse has required fields
	response := socketio.MixerModeResponse{
		Enabled: true,
		Success: true,
		Error:   "",
	}

	if !response.Enabled {
		t.Error("Expected Enabled to be true")
	}
	if !response.Success {
		t.Error("Expected Success to be true")
	}
	if response.Error != "" {
		t.Error("Expected Error to be empty")
	}
}

func TestGetMixerMode(t *testing.T) {
	// GetMixerMode should return a valid response (even if config doesn't exist)
	response := socketio.GetMixerMode()

	// Should return a response with Success set (may be false if config doesn't exist)
	// Enabled should be boolean (either true or false)
	t.Logf("Mixer enabled: %v, success: %v, error: %s", response.Enabled, response.Success, response.Error)

	// On dev machine without /etc/mpd.conf, expect Success=false
	// On Pi with /etc/mpd.conf, expect Success=true
}

func TestApplyBitPerfectResponseStructure(t *testing.T) {
	// Test that ApplyBitPerfectResponse has required fields
	response := socketio.ApplyBitPerfectResponse{
		Success: true,
		Applied: []string{"mixer_type = bit-perfect", "auto_resample = no"},
		Errors:  []string{},
	}

	if !response.Success {
		t.Error("Expected Success to be true")
	}
	if len(response.Applied) != 2 {
		t.Errorf("Expected 2 applied settings, got %d", len(response.Applied))
	}
	if len(response.Errors) != 0 {
		t.Errorf("Expected 0 errors, got %d", len(response.Errors))
	}
}

func TestApplyBitPerfect(t *testing.T) {
	// ApplyBitPerfect should return a valid response (even if config doesn't exist)
	response := socketio.ApplyBitPerfect()

	// Should return a response (may fail on dev machine without /etc/mpd.conf)
	t.Logf("Apply bit-perfect success: %v, applied: %v, errors: %v", response.Success, response.Applied, response.Errors)

	// On dev machine without /etc/mpd.conf, expect an error
	// On Pi with /etc/mpd.conf, expect success
}

// Tests for mixer mode detection in config parsing

func TestCheckBitPerfectFromConfig_MixerTypeNone(t *testing.T) {
	// Config with mixer_type "none" - bit-perfect
	mpdConfig := `
audio_output {
	type            "alsa"
	name            "My ALSA Device"
	device          "hw:0,0"
	mixer_type      "none"
}
`
	status := socketio.CheckBitPerfectFromConfig(mpdConfig, "", "")

	// Should have "Mixer: disabled" in config
	found := false
	for _, cfg := range status.Config {
		if contains(cfg, "Mixer") && contains(cfg, "disabled") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected 'Mixer: disabled' in config, got: %v", status.Config)
	}

	// Should not have mixer warning
	for _, warning := range status.Warnings {
		if contains(warning, "Mixer") && contains(warning, "software") {
			t.Errorf("Did not expect software mixer warning, got: %v", status.Warnings)
		}
	}
}

func TestCheckBitPerfectFromConfig_MixerTypeSoftware(t *testing.T) {
	// Config with mixer_type "software" - not bit-perfect
	mpdConfig := `
audio_output {
	type            "alsa"
	name            "My ALSA Device"
	device          "hw:0,0"
	mixer_type      "software"
}
`
	status := socketio.CheckBitPerfectFromConfig(mpdConfig, "", "")

	// Should have software mixer warning
	found := false
	for _, warning := range status.Warnings {
		if contains(warning, "Mixer") || contains(warning, "mixer") || contains(warning, "software") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected software mixer warning, got warnings: %v", status.Warnings)
	}
}

func TestCheckBitPerfectFromConfig_MixerTypeVariableWhitespace(t *testing.T) {
	// Test various whitespace formats for mixer_type
	testCases := []struct {
		name      string
		config    string
		expectOk  bool
	}{
		{
			name: "tabs",
			config: `audio_output {
	mixer_type	"none"
}`,
			expectOk: true,
		},
		{
			name: "multiple spaces",
			config: `audio_output {
    mixer_type      "none"
}`,
			expectOk: true,
		},
		{
			name: "two spaces",
			config: `audio_output {
    mixer_type  "none"
}`,
			expectOk: true,
		},
		{
			name: "single space",
			config: `audio_output {
    mixer_type "none"
}`,
			expectOk: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			status := socketio.CheckBitPerfectFromConfig(tc.config, "", "")

			// Check if mixer is detected as disabled (bit-perfect)
			found := false
			for _, cfg := range status.Config {
				if contains(cfg, "Mixer") && contains(cfg, "disabled") {
					found = true
					break
				}
			}
			if tc.expectOk && !found {
				t.Errorf("Expected 'Mixer: disabled' in config for %s, got: %v", tc.name, status.Config)
			}
		})
	}
}
