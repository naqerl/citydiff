package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
)

// The viewer opens itself in the default browser once it listens, so
// `citydiff -path .` lands on the city without a copy and paste. A container,
// a machine with no desktop, or a viewer restarted over and over does not want
// that, and says so in the environment: CITYDIFF_NO_BROWSER, set to anything,
// keeps the browser shut.
const noBrowserEnv = "CITYDIFF_NO_BROWSER"

// browserURL is the address a browser on this machine reaches the listener
// at. A wildcard host (0.0.0.0, ::, or none) is not an address to visit, so
// it becomes the loopback.
func browserURL(addr net.Addr) string {
	host, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return "http://" + addr.String() + "/"
	}
	if ip := net.ParseIP(host); host == "" || ip != nil && ip.IsUnspecified() {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/"
}

// browserCommand is the command that opens url in the default browser, or nil
// when there is none to ask. On Linux and the BSDs xdg-open belongs to a
// desktop: with no display it falls back to a text browser in this very
// terminal, so it is only asked when there is a display.
func browserCommand(goos, url string, getenv func(string) string) []string {
	if getenv(noBrowserEnv) != "" {
		return nil
	}
	switch goos {
	case "darwin":
		return []string{"open", url}
	case "windows":
		return []string{"rundll32", "url.dll,FileProtocolHandler", url}
	}
	if getenv("DISPLAY") == "" && getenv("WAYLAND_DISPLAY") == "" {
		return nil
	}
	return []string{"xdg-open", url}
}

// openBrowser starts the browser and does not wait for it. A browser that
// does not open costs a line on stderr; the viewer still serves.
func openBrowser(url string) {
	args := browserCommand(runtime.GOOS, url, os.Getenv)
	if args == nil {
		return
	}
	cmd := exec.Command(args[0], args[1:]...)
	detach(cmd)
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "citydiff: cannot open a browser: %v\n", err)
		return
	}
	go func() {
		if err := cmd.Wait(); err != nil {
			fmt.Fprintf(os.Stderr, "citydiff: cannot open a browser: %s: %v\n", args[0], err)
		}
	}()
}
