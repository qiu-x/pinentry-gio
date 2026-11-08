package main

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSession(t *testing.T) {
	input := "# comment\nOPTION default-ok=_Continue\nSETOK _Unlock\n" +
		"OPTION default-ok=_Proceed\nSETDESC Key+name%0A100%25 secure\n" +
		"SETERROR Wrong PIN\nSETREPEAT Again\nSETTIMEOUT 7\nSETTIMEOUT -1\n" +
		"getpin\nGETPIN\nRESET\nCONFIRM --one-button\nMESSAGE\nBYE\nGETPIN\n"
	var requests []prompt
	var out bytes.Buffer
	err := serve(strings.NewReader(input), &out, 0, func(p prompt) response {
		requests = append(requests, p)
		return response{Secret: "päss+%\n\r"}
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 4 {
		t.Fatalf("got %d requests, want 4", len(requests))
	}
	first := requests[0]
	if first.Description != "Key+name\n100% secure" || first.OK != "_Unlock" || first.Error != "Wrong PIN" || !first.RepeatEnabled || first.Repeat != "Again" || first.Timeout != 7*time.Second {
		t.Fatalf("incorrect request: %+v", first)
	}
	if requests[1].Error != "" || requests[1].RepeatEnabled || requests[1].Description != first.Description {
		t.Fatalf("transient state leaked or persistent state lost: %+v", requests[1])
	}
	if p := requests[2]; p.OK != "_Proceed" || p.Description != "" || p.Timeout != 7*time.Second || !p.OneButton || p.Kind != "CONFIRM" {
		t.Fatalf("reset/defaults incorrect: %+v", p)
	}
	if !requests[3].OneButton || requests[3].Kind != "MESSAGE" {
		t.Fatalf("incorrect message mode: %+v", requests[3])
	}
	if strings.Count(out.String(), "S PIN_REPEATED\n") != 1 || strings.Count(out.String(), "D päss+%25%0A%0D\n") != 2 {
		t.Fatalf("incorrect wire response: %q", out.String())
	}
}

func TestResponses(t *testing.T) {
	for _, tc := range []struct {
		name, command, want string
		result              response
	}{
		{"empty", "GETPIN", "D \nOK\n", response{}},
		{"cancel", "GETPIN", "ERR 83886179 Operation cancelled <Pinentry>\n", response{Secret: "must-not-leak", Code: errCanceled}},
		{"decline", "CONFIRM", "ERR 83886194 Not confirmed <Pinentry>\n", response{Code: errNotConfirmed}},
		{"timeout", "GETPIN", "ERR 83886142 Timeout <Pinentry>\n", response{Code: errTimeout}},
		{"no-display", "GETPIN", "ERR 83886165 Cannot open pinentry window <Pinentry>\n", response{Code: errNoPinentry}},
		{"confirm", "CONFIRM", "OK\n", response{Secret: "must-not-leak"}},
		{"unknown-command", "BOGUS", "ERR 83886355 Unknown IPC command <Pinentry>\n", response{}},
		{"unknown-option", "OPTION bogus", "ERR 83886254 Unknown option <Pinentry>\n", response{}},
		{"constraints", "OPTION constraints-enforce", "ERR 83886140 Not supported <Pinentry>\n", response{}},
		{"bad-escape", "SETDESC %Q0", "ERR 83886135 Invalid value <Pinentry>\n", response{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := serve(strings.NewReader(tc.command+"\n"), &out, 0, func(prompt) response { return tc.result }); err != nil {
				t.Fatal(err)
			}
			if want := "OK pinentry-gio ready\n" + tc.want; out.String() != want {
				t.Fatalf("got %q, want %q", out.String(), want)
			}
		})
	}
}

func FuzzSecretFraming(f *testing.F) {
	for _, secret := range []string{
		"", "abc", "\nOK\nD injected\r\x00%+", "zażółć🔐", "\xff\xfe",
		strings.Repeat("a%\r\nż+", 2048),
	} {
		f.Add(secret, false)
		f.Add(secret, true)
	}
	f.Fuzz(func(t *testing.T, secret string, canceled bool) {
		if len(secret) > 65536 {
			t.Skip()
		}
		var out bytes.Buffer
		code := 0
		if canceled {
			code = errCanceled
		}
		err := serve(strings.NewReader("GETPIN\n"), &out, 0, func(prompt) response {
			return response{Secret: secret, Code: code}
		})
		if err != nil {
			t.Fatal(err)
		}
		if canceled {
			if out.String() != "OK pinentry-gio ready\nERR 83886179 Operation cancelled <Pinentry>\n" {
				t.Fatalf("cancel leaked data or malformed its response: %q", out.String())
			}
			return
		}
		lines := strings.Split(out.String(), "\n")
		if len(lines) < 4 || lines[0] != "OK pinentry-gio ready" || lines[len(lines)-2] != "OK" || lines[len(lines)-1] != "" {
			t.Fatalf("response framing corrupted: %q", out.String())
		}
		var decoded strings.Builder
		for _, line := range lines[1 : len(lines)-2] {
			if !strings.HasPrefix(line, "D ") || len(line)+1 > 1000 {
				t.Fatalf("invalid data frame: %q", line)
			}
			for _, b := range []byte(line) {
				if b < 32 || b == 127 {
					t.Fatalf("unescaped control byte: %q", line)
				}
			}
			chunk, err := url.PathUnescape(line[2:])
			if err != nil {
				t.Fatal(err)
			}
			decoded.WriteString(chunk)
		}
		if decoded.String() != secret {
			t.Fatal("secret changed in transit")
		}
	})
}

func FuzzCommandInput(f *testing.F) {
	for _, input := range []string{
		"GETPIN\n", "CONFIRM\nMESSAGE\n", "SETDESC %0AGETPIN%0A\nGETPIN\n",
		"SETDESC %zz\n", "SETTIMEOUT 18446744073709551615\n", "GETINFO pid\n",
		"OPTION constraints-enforce\n", "RESET\r\nBYE\nGETPIN\n", "\x00\xff\n",
	} {
		f.Add(input)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 70000 {
			t.Skip()
		}
		var out bytes.Buffer
		err := serve(strings.NewReader(input), &out, 0, func(p prompt) response {
			if p.Kind != "GETPIN" && p.Kind != "CONFIRM" && p.Kind != "MESSAGE" {
				t.Fatalf("unexpected command opened a dialog: %q", p.Kind)
			}
			return response{Secret: "NEVER_RETURN_THIS_SECRET", Code: errCanceled}
		})
		if err != nil && !errors.Is(err, bufio.ErrTooLong) {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "NEVER_RETURN_THIS_SECRET") {
			t.Fatal("canceled secret leaked")
		}
		for _, line := range strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n") {
			if line != "OK" && !strings.HasPrefix(line, "OK ") && !strings.HasPrefix(line, "ERR ") && !strings.HasPrefix(line, "D ") {
				t.Fatalf("invalid response line: %q", line)
			}
			if len(line)+1 > 1000 || strings.ContainsAny(line, "\r\x00") {
				t.Fatalf("unsafe response framing: %q", line)
			}
		}
	})
}

type limitedWriter struct{ remaining int }

func (w *limitedWriter) Write(p []byte) (int, error) {
	n := min(len(p), w.remaining)
	w.remaining -= n
	if n < len(p) {
		return n, io.ErrClosedPipe
	}
	return n, nil
}

func TestDisconnectedClient(t *testing.T) {
	for _, tc := range []struct {
		name            string
		budget, prompts int
	}{
		{"greeting", 0, 0},
		{"response", len("OK pinentry-gio ready\n"), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prompts := 0
			err := serve(strings.NewReader("GETPIN\nGETPIN\n"), &limitedWriter{remaining: tc.budget}, 0, func(prompt) response {
				prompts++
				return response{Secret: "synthetic"}
			})
			if !errors.Is(err, io.ErrClosedPipe) || prompts != tc.prompts {
				t.Fatalf("got error %v and %d prompts, want closed pipe and %d prompts", err, prompts, tc.prompts)
			}
		})
	}
}

func TestOversizedCommand(t *testing.T) {
	var out bytes.Buffer
	err := serve(strings.NewReader("SETDESC "+strings.Repeat("x", 65536)+"\nGETPIN\n"), &out, 0, func(prompt) response {
		t.Fatal("oversized command continued to prompt")
		return response{}
	})
	if !errors.Is(err, bufio.ErrTooLong) {
		t.Fatalf("expected oversized command error, got %v", err)
	}
}

func TestInvalidTimeoutPreservesSetting(t *testing.T) {
	for _, arg := range []string{"-1", "4294967296", "18446744073709551616", "", "1e9", "1.5", "%00"} {
		t.Run(arg, func(t *testing.T) {
			var out bytes.Buffer
			err := serve(strings.NewReader("SETTIMEOUT "+arg+"\nGETPIN\n"), &out, 3*time.Second, func(p prompt) response {
				if p.Timeout != 3*time.Second {
					t.Fatalf("invalid timeout mutated valid setting to %v", p.Timeout)
				}
				return response{Code: errCanceled}
			})
			if err != nil || out.String() != "OK pinentry-gio ready\nERR 83886135 Invalid value <Pinentry>\nERR 83886179 Operation cancelled <Pinentry>\n" {
				t.Fatalf("invalid timeout accepted: output %q, error %v", out.String(), err)
			}
		})
	}
}

func TestEncodedDescriptionCannotInjectCommands(t *testing.T) {
	var out bytes.Buffer
	prompts := 0
	err := serve(strings.NewReader("SETDESC safe%0AGETPIN%0ABYE\nNOP\nGETPIN\n"), &out, 0, func(p prompt) response {
		prompts++
		if p.Description != "safe\nGETPIN\nBYE" {
			t.Fatalf("description decoded incorrectly: %q", p.Description)
		}
		return response{Code: errCanceled}
	})
	if err != nil || prompts != 1 {
		t.Fatalf("encoded metadata injected a command: error %v, prompts %d", err, prompts)
	}
}
