package winui

import "os/exec"

// OpenAppWindow opens url in a window of its own with no tabs or address
// bar (an Edge app window), so a page such as the office can sit on another
// screen. The window uses an Edge profile of its own in profileDir: the page
// holds the window's sign-in to baton, which the extensions of the user's
// everyday profile must not see. Without Edge it opens in the default
// browser.
func OpenAppWindow(url, profileDir string) error {
	if edge := FindEdge(); edge != "" {
		LetOthersComeForward()
		cmd := exec.Command(edge, "--user-data-dir="+profileDir, "--no-first-run", "--no-default-browser-check",
			"--app="+url, "--window-size=1400,900")
		if err := cmd.Start(); err == nil {
			_ = cmd.Process.Release()
			return nil
		}
	}
	return OpenURL(url)
}
