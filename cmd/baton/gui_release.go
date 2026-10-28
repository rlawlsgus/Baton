//go:build !batontest

package main

import "flag"

// Release builds have no test switches: the window always opens, and every
// sign-in and session gets its real window.
func guiTestFlags(*flag.FlagSet) (noWindow, testSpawn *bool) {
	off := false
	return &off, &off
}
