//go:build windows

package lcd

type windowsController struct{}

func newPlatform() Controller                        { return &windowsController{} }
func (c *windowsController) Status() (Status, error) { return Status{IsOn: true}, nil }
func (c *windowsController) Set(on bool) error       { return ErrUnsupported }
