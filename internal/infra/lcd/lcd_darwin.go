//go:build darwin

package lcd

type darwinController struct{}

func newPlatform() Controller                       { return &darwinController{} }
func (c *darwinController) Status() (Status, error) { return Status{IsOn: true}, nil }
func (c *darwinController) Set(on bool) error       { return ErrUnsupported }
