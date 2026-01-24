package qobuz

import (
	"testing"
)

func TestDecodeSecret(t *testing.T) {
	// Test with simple base64 values
	// These are just test values, not real secrets
	seed := "dGVzdA==" // "test" in base64
	info := "c2VlZA==" // "seed" in base64
	extras := "ZGF0YQ==" // "data" in base64

	result, err := decodeSecret(seed, info, extras)
	if err != nil {
		t.Fatalf("decodeSecret() error = %v", err)
	}

	expected := "testseeddata"
	if result != expected {
		t.Errorf("decodeSecret() = %v, want %v", result, expected)
	}
}

func TestDecodeSecretInvalidBase64(t *testing.T) {
	// Invalid base64 should return error
	_, err := decodeSecret("not-valid-base64!!!", "c2VlZA==", "ZGF0YQ==")
	if err == nil {
		t.Error("decodeSecret() should fail with invalid base64")
	}
}

func TestCreateSignedRequest(t *testing.T) {
	appSecret := "testsecret123"
	method := "track/getFileUrl"
	params := map[string]string{
		"track_id": "123456",
		"format":   "5",
	}

	timestamp, signature := CreateSignedRequest(appSecret, method, params)

	// Verify timestamp is not empty
	if timestamp == "" {
		t.Error("timestamp should not be empty")
	}

	// Verify signature is a 32-char hex string (MD5)
	if len(signature) != 32 {
		t.Errorf("signature length = %v, want 32", len(signature))
	}

	// Verify signature contains only hex characters
	for _, c := range signature {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("signature contains non-hex character: %c", c)
			break
		}
	}
}

// TestExtractWebPlayerCredentials is an integration test that hits the real Qobuz server.
// It should be skipped in CI/automated testing.
func TestExtractWebPlayerCredentials(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	creds, err := ExtractWebPlayerCredentials()
	if err != nil {
		t.Logf("ExtractWebPlayerCredentials() error = %v (this may be expected if Qobuz changes their web player)", err)
		t.Skip("Web player credential extraction failed - this is expected if Qobuz updated their player")
	}

	if creds.AppID == "" {
		t.Error("AppID should not be empty")
	}

	if len(creds.AppID) != 9 {
		t.Errorf("AppID length = %v, want 9", len(creds.AppID))
	}

	if creds.AppSecret == "" {
		t.Error("AppSecret should not be empty")
	}

	t.Logf("Successfully extracted credentials: AppID=%s (secret hidden)", creds.AppID)
}

func TestFindBundleURL(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	bundleURL, err := findBundleURL()
	if err != nil {
		t.Logf("findBundleURL() error = %v (this may be expected if Qobuz changes their web player)", err)
		t.Skip("Bundle URL extraction failed")
	}

	if bundleURL == "" {
		t.Error("bundleURL should not be empty")
	}

	// Verify it looks like a valid URL
	if bundleURL[:4] != "http" {
		t.Errorf("bundleURL should start with http, got %v", bundleURL[:4])
	}

	t.Logf("Found bundle URL: %s", bundleURL)
}
