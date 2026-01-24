// Package qobuz provides Qobuz streaming service integration.
package qobuz

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const (
	qobuzLoginPageURL = "https://play.qobuz.com/login"
	qobuzAPIBaseURL   = "https://www.qobuz.com/api.json/0.2"
)

// WebPlayerCredentials holds credentials extracted from the Qobuz web player.
type WebPlayerCredentials struct {
	AppID     string
	AppSecret string
}

// ExtractWebPlayerCredentials extracts Qobuz API credentials from the web player.
// This method is less stable than official API credentials but works for development.
func ExtractWebPlayerCredentials() (*WebPlayerCredentials, error) {
	// Step 1: Fetch the login page to find the bundle URL
	bundleURL, err := findBundleURL()
	if err != nil {
		return nil, fmt.Errorf("failed to find bundle URL: %w", err)
	}

	// Step 2: Fetch the bundle and extract credentials
	appID, secrets, err := extractCredentialsFromBundle(bundleURL)
	if err != nil {
		return nil, fmt.Errorf("failed to extract credentials from bundle: %w", err)
	}

	// Step 3: Validate which secret works
	validSecret, err := validateSecret(appID, secrets)
	if err != nil {
		return nil, fmt.Errorf("failed to validate secret: %w", err)
	}

	return &WebPlayerCredentials{
		AppID:     appID,
		AppSecret: validSecret,
	}, nil
}

// findBundleURL fetches the login page and finds the JavaScript bundle URL.
func findBundleURL() (string, error) {
	resp, err := http.Get(qobuzLoginPageURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Look for the bundle script URL pattern
	// e.g., <script src="/resources/XXX/bundle.js">
	bundlePattern := regexp.MustCompile(`<script[^>]+src="([^"]*bundle[^"]*\.js)"`)
	matches := bundlePattern.FindSubmatch(body)
	if len(matches) < 2 {
		// Try alternate pattern for newer versions
		bundlePattern = regexp.MustCompile(`"(/resources/\d+\.\d+\.\d+-[^/]+/bundle\.js)"`)
		matches = bundlePattern.FindSubmatch(body)
		if len(matches) < 2 {
			return "", fmt.Errorf("bundle URL not found in login page")
		}
	}

	bundlePath := string(matches[1])
	if !strings.HasPrefix(bundlePath, "http") {
		bundlePath = "https://play.qobuz.com" + bundlePath
	}

	return bundlePath, nil
}

// extractCredentialsFromBundle fetches the bundle and extracts the app ID and secrets.
func extractCredentialsFromBundle(bundleURL string) (string, []string, error) {
	resp, err := http.Get(bundleURL)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, err
	}

	bundleStr := string(body)

	// Extract App ID
	// Pattern: production:{api:{appId:"XXXXXXXXX"
	appIDPattern := regexp.MustCompile(`production:\{api:\{appId:"(\d{9})"`)
	appIDMatches := appIDPattern.FindStringSubmatch(bundleStr)
	if len(appIDMatches) < 2 {
		// Try alternate pattern
		appIDPattern = regexp.MustCompile(`app_id:\s*["'](\d{9})["']`)
		appIDMatches = appIDPattern.FindStringSubmatch(bundleStr)
		if len(appIDMatches) < 2 {
			return "", nil, fmt.Errorf("app ID not found in bundle")
		}
	}
	appID := appIDMatches[1]

	// Extract secrets
	// The secrets are base64-encoded and combined from seed, info, and extras
	secrets, err := extractSecrets(bundleStr)
	if err != nil {
		return "", nil, fmt.Errorf("failed to extract secrets: %w", err)
	}

	return appID, secrets, nil
}

// extractSecrets extracts and decodes the secrets from the bundle.
func extractSecrets(bundleStr string) ([]string, error) {
	var secrets []string

	// Look for timezone-based secret patterns
	// The secrets are typically encoded in patterns like:
	// {offset:"...",timezone:"...",seed:"base64",info:"base64",extras:"base64"}
	secretPattern := regexp.MustCompile(`\{[^{}]*?seed:"([^"]+)"[^{}]*?info:"([^"]+)"[^{}]*?extras:"([^"]+)"[^{}]*?\}`)
	matches := secretPattern.FindAllStringSubmatch(bundleStr, -1)

	for _, match := range matches {
		if len(match) >= 4 {
			seed := match[1]
			info := match[2]
			extras := match[3]

			secret, err := decodeSecret(seed, info, extras)
			if err != nil {
				continue // Skip invalid secrets
			}
			secrets = append(secrets, secret)
		}
	}

	// Also try the simpler pattern for direct secrets
	directSecretPattern := regexp.MustCompile(`["']([a-f0-9]{32})["']`)
	directMatches := directSecretPattern.FindAllStringSubmatch(bundleStr, -1)
	for _, match := range directMatches {
		if len(match) >= 2 {
			// Check if it looks like a valid secret (32 hex chars)
			if len(match[1]) == 32 {
				// Avoid duplicates
				found := false
				for _, s := range secrets {
					if s == match[1] {
						found = true
						break
					}
				}
				if !found {
					secrets = append(secrets, match[1])
				}
			}
		}
	}

	if len(secrets) == 0 {
		return nil, fmt.Errorf("no secrets found in bundle")
	}

	return secrets, nil
}

// decodeSecret decodes a secret from its seed, info, and extras components.
func decodeSecret(seed, info, extras string) (string, error) {
	// Decode base64 components
	seedBytes, err := base64.StdEncoding.DecodeString(seed)
	if err != nil {
		return "", err
	}
	infoBytes, err := base64.StdEncoding.DecodeString(info)
	if err != nil {
		return "", err
	}
	extrasBytes, err := base64.StdEncoding.DecodeString(extras)
	if err != nil {
		return "", err
	}

	// Combine to form the secret
	// The exact combination method may vary, but typically it's concatenation
	combined := string(seedBytes) + string(infoBytes) + string(extrasBytes)

	return combined, nil
}

// validateSecret tests each secret against the Qobuz API to find one that works.
func validateSecret(appID string, secrets []string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	for _, secret := range secrets {
		if testSecret(client, appID, secret) {
			return secret, nil
		}
	}

	return "", fmt.Errorf("no valid secret found")
}

// testSecret tests if a secret is valid by making a signed API request.
func testSecret(client *http.Client, appID, secret string) bool {
	// Create a test request to track/getFileUrl
	// This endpoint requires a valid signature
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	trackID := "5966783" // A known public track ID for testing

	// Build the signature
	// Format: track_idXXXformatXXXintentXXXrequest_tsXXXsecret
	sigInput := fmt.Sprintf("track/getFileUrlformat5intentstreamtrack_id%srequest_ts%s%s",
		trackID, timestamp, secret)
	hash := md5.Sum([]byte(sigInput))
	signature := hex.EncodeToString(hash[:])

	// Make the request
	url := fmt.Sprintf("%s/track/getFileUrl?track_id=%s&format_id=5&intent=stream&request_ts=%s&request_sig=%s&app_id=%s",
		qobuzAPIBaseURL, trackID, timestamp, signature, appID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	// A valid secret will return something other than 400
	// Note: We may get 401 if not logged in, but that's expected
	return resp.StatusCode != 400
}

// CreateSignedRequest creates a signed request for the Qobuz API.
func CreateSignedRequest(appSecret, method string, params map[string]string) (string, string) {
	timestamp := fmt.Sprintf("%d", time.Now().Unix())

	// Build signature input: method + sorted params + timestamp + secret
	sigParts := []string{method}
	for key, value := range params {
		sigParts = append(sigParts, key+value)
	}
	sigParts = append(sigParts, "request_ts"+timestamp, appSecret)

	sigInput := strings.Join(sigParts, "")
	hash := md5.Sum([]byte(sigInput))
	signature := hex.EncodeToString(hash[:])

	return timestamp, signature
}
