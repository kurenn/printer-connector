//go:build !windows

package main

import "errors"

var errWindowsOnly = errors.New("service mode is Windows-only")

// runService is a no-op stub on non-Windows platforms.
func runService(_ string) error { return errWindowsOnly }

// installService is a no-op stub on non-Windows platforms.
func installService(_, _ string) error { return errWindowsOnly }

// removeService is a no-op stub on non-Windows platforms.
func removeService() error { return errWindowsOnly }

// windowsConfigPath is a stub on non-Windows platforms; it returns the empty
// string because the Windows ProgramData layout does not exist here.
func windowsConfigPath() string { return "" }
