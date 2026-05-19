//go:build windows

package lcd

import "os"

type windowsController struct{}

func (c *windowsController) Status() (Status, error) { return Status{IsOn: true}, nil }
func (c *windowsController) Set(on bool) error       { return ErrUnsupported }

func newPlatform() Controller {
	url := os.Getenv("STELLAR_LCD_REMOTE_URL")
	tok := os.Getenv("STELLAR_LCD_REMOTE_TOKEN")
	if url == "" || tok == "" {
		return &windowsController{}
	}
	return NewRemoteController(url, tok)
}
