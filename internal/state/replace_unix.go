//go:build !windows

package state

import "os"

func replace(src, dst string) error { return os.Rename(src, dst) }
