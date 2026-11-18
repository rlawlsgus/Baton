//go:build batontest

package main

// The test build serves the window for checks by hand and for the UI tests,
// often with only BATON_HOME pointed at a scratch folder: before anything
// else runs, it fakes the Recycle Bin and the Claude desktop app (see
// fakeOutsideWorld).
func init() { fakeOutsideWorld() }
