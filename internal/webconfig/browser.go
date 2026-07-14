package webconfig

import (
	"os/exec"
	"runtime"
)

// openBrowser best-effort opens url in the user's default browser. Any
// failure (missing binary, exec error, unsupported OS) is silently
// ignored - the server keeps running either way.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return
	}
	_ = cmd.Start()
}
