//go:build darwin

package lcd

import "os"

// darwinController is the fallback when no remote URL is configured.
// Preserves M1.A's "Mac compiles & boots cleanly without env file" guarantee:
// go test ./... on a fresh dev Mac sees this stub and stays green.
type darwinController struct{}

func (c *darwinController) Status() (Status, error) { return Status{IsOn: true}, nil }
func (c *darwinController) Set(on bool) error       { return ErrUnsupported }

// newPlatform returns a RemoteController when STELLAR_LCD_REMOTE_URL +
// STELLAR_LCD_REMOTE_TOKEN are both set, otherwise the local stub.
func newPlatform() Controller {
	url := os.Getenv("STELLAR_LCD_REMOTE_URL")
	tok := os.Getenv("STELLAR_LCD_REMOTE_TOKEN")
	if url == "" || tok == "" {
		return &darwinController{}
	}
	return NewRemoteController(url, tok)
}
