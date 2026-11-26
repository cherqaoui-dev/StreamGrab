//go:build !windows

package platform

import "errors"

func FreeBytes(string) (uint64, error) {
	return 0, errors.New("disk query is unavailable")
}
