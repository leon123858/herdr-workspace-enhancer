//go:build !windows

package config

import "os"

func replace(src, dst string) error { return os.Rename(src, dst) }
