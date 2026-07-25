package utils

import (
	"fmt"
	"os/exec"
	"runtime"
)

func OSExec(urlPath string) error {
	var oserr error
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", urlPath)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", urlPath)
	case "darwin":
		cmd = exec.Command("open", urlPath)
	default:
		return fmt.Errorf("unsupported platform")
	}
	oserr = cmd.Start()
	if oserr != nil {
		return oserr
	}
	go func() {
		_ = cmd.Wait()
	}()
	return oserr
}
