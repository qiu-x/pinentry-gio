package theme

import (
	"bufio"
	"os/exec"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

func TestDesktopThemePreference(t *testing.T) {
	daemon, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon is required for the isolated portal test")
	}
	cmd := exec.Command(daemon, "--session", "--nofork", "--print-address=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() {
		t.Fatal("test session bus did not publish its address")
	}
	address := scanner.Text()
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", address)
	conn, err := dbus.Connect(address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if reply, err := conn.RequestName("org.freedesktop.portal.Desktop", dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("could not own test portal: reply %v, error %v", reply, err)
	}
	for _, tc := range []struct {
		name  string
		value any
		err   *dbus.Error
		delay time.Duration
		dark  bool
	}{
		{name: "dark", value: uint32(1), dark: true},
		{name: "light", value: uint32(2)},
		{name: "no preference", value: uint32(0)},
		{name: "unknown value", value: uint32(42)},
		{name: "wrong type", value: "1"},
		{name: "missing setting", value: uint32(1), err: dbus.NewError("org.freedesktop.portal.Error.NotFound", nil)},
		{name: "unresponsive portal", value: uint32(1), delay: time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := conn.ExportMethodTable(map[string]any{
				"ReadOne": func(namespace, key string) (dbus.Variant, *dbus.Error) {
					if namespace != "org.freedesktop.appearance" || key != "color-scheme" {
						return dbus.Variant{}, dbus.NewError("org.freedesktop.portal.Error.NotFound", nil)
					}
					time.Sleep(tc.delay)
					return dbus.MakeVariant(tc.value), tc.err
				},
			}, "/org/freedesktop/portal/desktop", "org.freedesktop.portal.Settings"); err != nil {
				t.Fatal(err)
			}
			if got := prefersDark(); got != tc.dark {
				t.Fatalf("dark preference = %v, want %v", got, tc.dark)
			}
		})
	}
	t.Run("no session bus", func(t *testing.T) {
		t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+t.TempDir()+"/missing-bus")
		if prefersDark() {
			t.Fatal("missing session bus must fall back to light")
		}
	})
}
