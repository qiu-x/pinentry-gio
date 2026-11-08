package main

import (
	"flag"
	"fmt"
	"os"

	"gioui.org/app"
)

func main() {
	showVersion := flag.Bool("version", false, "print version")
	demo := flag.Bool("demo", false, "preview a dialog without GPG (never prints the entered text)")
	timeout := flag.String("timeout", "0", "dialog timeout in seconds; 0 disables it")
	display := flag.String("display", "", "X11 display")
	for _, name := range []string{"ttyname", "ttytype", "lc-ctype", "lc-messages", "parent-wid"} {
		flag.String(name, "", "accepted for pinentry compatibility; unused")
	}
	flag.Bool("no-global-grab", false, "accepted for compatibility; input is always window-local")
	flag.Parse()
	if *showVersion {
		fmt.Println("pinentry-gio " + version)
		return
	}
	duration, code := parseTimeout(*timeout)
	if code != 0 || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "pinentry-gio: invalid arguments")
		os.Exit(2)
	}
	if *display != "" {
		if err := os.Setenv("DISPLAY", *display); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	go func() {
		if *demo {
			showDialog(prompt{
				Kind: "GETPIN", Description: "Enter the passphrase to unlock your OpenPGP key.\n\nFoo Bar <foo@example.com>\nED25519 · created 2026-11-08",
				Label: "Passphrase", OK: "Unlock", Cancel: "Cancel", Timeout: duration,
			})
		} else if err := serve(os.Stdin, os.Stdout, duration, showDialog); err != nil {
			fmt.Fprintln(os.Stderr, "pinentry-gio:", err)
			os.Exit(1)
		}
		os.Exit(0)
	}()
	app.Main()
}
