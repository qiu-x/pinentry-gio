package main

import (
	"bufio"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const version = "0.1.0"

// libgpg-error codes
const (
	errInvalid      = 55
	errUnsupported  = 60
	errTimeout      = 62
	errNoPinentry   = 85
	errCanceled     = 99
	errNotConfirmed = 114
	errOption       = 174
	errCommand      = 275
)

type prompt struct {
	Kind, Title, Description, Label, Error string
	OK, Cancel, NotOK                      string
	Repeat, RepeatError                    string
	RepeatEnabled, OneButton               bool
	Timeout                                time.Duration
}

type response struct {
	Secret string
	Code   int
}

func serve(in io.Reader, out io.Writer, timeout time.Duration, ask func(prompt) response) error {
	w := bufio.NewWriter(out)
	line := func(s string) { _, _ = fmt.Fprintln(w, s) }
	data := func(s string) {
		// Keep each libassuan line below its 1000-byte limit.
		const hex = "0123456789ABCDEF"
		chunk := make([]byte, 0, 990)
		for i := 0; i < len(s); i++ {
			b := s[i]
			if len(chunk) > 986 {
				line("D " + string(chunk))
				chunk = chunk[:0]
			}
			if b == '%' || b < 32 || b == 127 {
				chunk = append(chunk, '%', hex[b>>4], hex[b&15])
			} else {
				chunk = append(chunk, b)
			}
		}
		line("D " + string(chunk))
		clear(chunk)
	}
	defaults := prompt{Label: "Passphrase", OK: "OK", Cancel: "Cancel", Timeout: timeout}
	p := prompt{Timeout: timeout}
	line("OK pinentry-gio ready")
	if err := w.Flush(); err != nil {
		return err
	}
	s := bufio.NewScanner(in)
	s.Buffer(make([]byte, 4096), 64*1024)
	for s.Scan() {
		input := strings.TrimLeft(s.Text(), " \t")
		if input == "" || strings.HasPrefix(input, "#") {
			continue
		}
		cmd, arg, _ := strings.Cut(input, " ")
		arg = strings.TrimLeft(arg, " ")
		value, decodeErr := url.PathUnescape(arg) // '+' is literal in libassuan.
		code := 0
		if decodeErr != nil {
			code = errInvalid
		} else {
			switch strings.ToUpper(cmd) {
			case "BYE":
				line("OK closing connection")
				return w.Flush()
			case "NOP":
			case "RESET":
				p = prompt{Timeout: defaults.Timeout}
			case "SETTITLE":
				p.Title = value
			case "SETDESC":
				p.Description = value
			case "SETPROMPT":
				p.Label = value
			case "SETERROR":
				p.Error = value
			case "SETOK":
				p.OK = value
			case "SETCANCEL":
				p.Cancel = value
			case "SETNOTOK":
				p.NotOK = value
			case "SETREPEAT":
				p.Repeat, p.RepeatEnabled = value, true
			case "SETREPEATERROR":
				p.RepeatError = value
			case "SETTIMEOUT":
				var duration time.Duration
				duration, code = parseTimeout(value)
				if code == 0 {
					p.Timeout, defaults.Timeout = duration, duration
				}
			case "SETKEYINFO", "SETREPEATOK", "SETQUALITYBAR", "SETQUALITYBAR_TT":
				// TODO
			case "OPTION":
				name, val, _ := strings.Cut(value, "=")
				switch name {
				case "default-ok":
					defaults.OK = val
				case "default-cancel":
					defaults.Cancel = val
				case "default-notok":
					defaults.NotOK = val
				case "default-prompt":
					defaults.Label = val
				case "ttyname", "ttytype", "ttyalert", "lc-ctype", "lc-messages", "owner", "parent-wid",
					"allow-external-password-cache", "grab", "no-grab",
					"default-pwmngr", "default-cf-visi", "default-tt-visi", "default-tt-hide", "default-capshint":
					// unsupported
				case "constraints-enforce", "formatted-passphrase":
					code = errUnsupported
				default:
					code = errOption
				}
			case "GETINFO":
				switch value {
				case "version":
					data(version)
				case "pid":
					data(strconv.Itoa(os.Getpid()))
				case "flavor":
					data("gio")
				default:
					code = errInvalid
				}
			case "GETPIN", "CONFIRM", "MESSAGE":
				p.Kind = strings.ToUpper(cmd)
				p.OneButton = p.Kind == "MESSAGE" || (p.Kind == "CONFIRM" && strings.Contains(arg, "--one-button"))
				request := p
				for dst, fallback := range map[*string]string{
					&request.Label: defaults.Label, &request.OK: defaults.OK,
					&request.Cancel: defaults.Cancel, &request.NotOK: defaults.NotOK,
				} {
					if *dst == "" {
						*dst = fallback
					}
				}
				r := ask(request)
				code = r.Code
				if code == 0 && p.Kind == "GETPIN" {
					if p.RepeatEnabled {
						line("S PIN_REPEATED")
					}
					data(r.Secret)
				}
				p.Error = ""
				if p.Kind == "GETPIN" {
					p.Repeat, p.RepeatEnabled = "", false
				}
			default:
				code = errCommand
			}
		}
		if code != 0 {
			line(fmt.Sprintf("ERR %d %s <Pinentry>", 5<<24|code, errorText(code)))
		} else {
			line("OK")
		}
		if err := w.Flush(); err != nil {
			return err
		}
	}
	return s.Err()
}

func parseTimeout(s string) (time.Duration, int) {
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, errInvalid
	}
	return time.Duration(n) * time.Second, 0
}

func errorText(code int) string {
	switch code {
	case errCanceled:
		return "Operation cancelled"
	case errNotConfirmed:
		return "Not confirmed"
	case errTimeout:
		return "Timeout"
	case errNoPinentry:
		return "Cannot open pinentry window"
	case errOption:
		return "Unknown option"
	case errCommand:
		return "Unknown IPC command"
	case errUnsupported:
		return "Not supported"
	default:
		return "Invalid value"
	}
}
