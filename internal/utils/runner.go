package utils

import (
	"fmt"
	"os/exec"
	"runtime"
)

func OSExec(urlPath string) error {
	var oserr error
	switch runtime.GOOS {
	case "linux":
		oserr = exec.Command("xdg-open", urlPath).Start()
	case "windows":
		oserr = exec.Command("rundll32", "url.dll,FileProtocolHandler", urlPath).Start()
	case "darwin":
		oserr = exec.Command("open", urlPath).Start()
	default:
		oserr = fmt.Errorf("unsupported platform")
	}
	return oserr
}
