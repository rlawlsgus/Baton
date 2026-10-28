//go:build batontest

package main

import "flag"

// Test builds (go build -tags batontest) can serve the GUI without a window
// and fake the windows it opens, for the UI tests.
func guiTestFlags(fs *flag.FlagSet) (noWindow, testSpawn *bool) {
	return fs.Bool("no-window", false, "only serve and print the address (testing)"),
		fs.Bool("test-spawn", false, "run sign-ins hidden and only print session starts (testing)")
}
