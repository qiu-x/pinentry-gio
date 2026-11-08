// Package theme selects the dialog colors from the desktop preference at startup.
package theme

import (
	"context"
	"image/color"
	"time"

	"github.com/godbus/dbus/v5"
)

var (
	Background = color.NRGBA{R: 247, G: 248, B: 250, A: 255}
	Ink        = color.NRGBA{R: 26, G: 35, B: 49, A: 255}
	Muted      = color.NRGBA{R: 98, G: 110, B: 128, A: 255}
	Accent     = color.NRGBA{R: 40, G: 89, B: 216, A: 255}
	Danger     = color.NRGBA{R: 180, G: 41, B: 57, A: 255}
	ButtonInk  = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
)

func init() {
	if prefersDark() {
		Background = color.NRGBA{R: 26, G: 26, B: 26, A: 255}
		Ink = color.NRGBA{R: 238, G: 238, B: 238, A: 255}
		Muted = color.NRGBA{R: 175, G: 175, B: 175, A: 255}
		Accent = color.NRGBA{R: 139, G: 180, B: 255, A: 255}
		Danger = color.NRGBA{R: 255, G: 137, B: 146, A: 255}
		ButtonInk = Background
	}
}

// Read the desktop preference. An unavailable portal, an unknown value,
// or no preference preserves the default light appearance.
func prefersDark() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	// Do not launch a session bus just to determine the color scheme.
	conn, err := dbus.SessionBusPrivateNoAutoStartup(dbus.WithContext(ctx))
	if err != nil {
		return false
	}
	defer conn.Close()
	if conn.Auth(nil) != nil || conn.Hello() != nil {
		return false
	}
	var value dbus.Variant
	err = conn.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop").
		CallWithContext(ctx, "org.freedesktop.portal.Settings.ReadOne", 0,
			"org.freedesktop.appearance", "color-scheme").Store(&value)
	return err == nil && value.Value() == uint32(1)
}
