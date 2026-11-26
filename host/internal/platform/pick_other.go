//go:build !windows

package platform

import "errors"

var ErrCancelled = errors.New("folder picker cancelled")

func PickDirectory() (string, error) {
	return "", errors.New("folder picker is unavailable on this system")
}
