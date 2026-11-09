//go:build !darwin && !windows

package update

import "errors"

// Install isn't supported on this platform yet.
func (r *Release) Install() error {
	return errors.New("update: Install isn't supported on this platform")
}
