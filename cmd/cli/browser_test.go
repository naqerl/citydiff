package main

import (
	"net"
	"slices"
	"testing"
)

func TestBrowserURL(t *testing.T) {
	for _, tc := range []struct {
		addr net.Addr
		want string
	}{
		{&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8787}, "http://127.0.0.1:8787/"},
		{&net.TCPAddr{IP: net.IPv4zero, Port: 8787}, "http://127.0.0.1:8787/"},
		{&net.TCPAddr{IP: net.IPv6unspecified, Port: 8787}, "http://127.0.0.1:8787/"},
		{&net.TCPAddr{Port: 9000}, "http://127.0.0.1:9000/"},
		{&net.TCPAddr{IP: net.IPv4(100, 64, 0, 7), Port: 8787}, "http://100.64.0.7:8787/"},
		{&net.TCPAddr{IP: net.IPv6loopback, Port: 8787}, "http://[::1]:8787/"},
	} {
		if got := browserURL(tc.addr); got != tc.want {
			t.Errorf("browserURL(%s) = %s, want %s", tc.addr, got, tc.want)
		}
	}
}

// TestBrowserURLListener reads the port a listener on :0 was given, which the
// address it was asked for does not name.
func TestBrowserURLListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	want := "http://" + ln.Addr().String() + "/"
	if got := browserURL(ln.Addr()); got != want {
		t.Fatalf("browserURL = %s, want %s", got, want)
	}
}

func TestBrowserCommand(t *testing.T) {
	const url = "http://127.0.0.1:8787/"
	env := func(vars map[string]string) func(string) string {
		return func(key string) string { return vars[key] }
	}
	for _, tc := range []struct {
		name string
		goos string
		env  map[string]string
		want []string
	}{
		{"mac", "darwin", nil, []string{"open", url}},
		{"windows", "windows", nil, []string{"rundll32", "url.dll,FileProtocolHandler", url}},
		{"x11", "linux", map[string]string{"DISPLAY": ":0"}, []string{"xdg-open", url}},
		{"wayland", "linux", map[string]string{"WAYLAND_DISPLAY": "wayland-1"}, []string{"xdg-open", url}},
		{"bsd desktop", "freebsd", map[string]string{"DISPLAY": ":0"}, []string{"xdg-open", url}},
		{"no display", "linux", nil, nil},
		{"turned off", "linux", map[string]string{"DISPLAY": ":0", noBrowserEnv: "1"}, nil},
		{"turned off on mac", "darwin", map[string]string{noBrowserEnv: "yes"}, nil},
	} {
		got := browserCommand(tc.goos, url, env(tc.env))
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: browserCommand = %q, want %q", tc.name, got, tc.want)
		}
	}
}
