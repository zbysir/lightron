//go:build !darwin && !windows

package lightron

// ErrNoBrowser is macOS's; declared here so code using it builds everywhere.
var ErrNoBrowser = ErrUnsupported

func (a *App) run() error { return ErrUnsupported }
