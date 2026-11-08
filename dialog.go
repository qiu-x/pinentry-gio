package main

import (
	"image"
	"strings"
	"sync/atomic"
	"time"

	"pinentry-gio/theme"

	"gioui.org/app"
	"gioui.org/font/gofont"
	"gioui.org/io/key"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

const dialogWidth, dialogHeight = unit.Dp(480), unit.Dp(420)

func showDialog(p prompt) response {
	var w app.Window
	title := "pinentry-gio"
	if p.Title != "" {
		title += " — " + p.Title
	}
	w.Option(app.Title(title), app.Size(dialogWidth, dialogHeight),
		app.MinSize(dialogWidth, dialogHeight), app.MaxSize(dialogWidth, dialogHeight))
	var expired atomic.Bool
	if p.Timeout > 0 {
		timer := time.AfterFunc(p.Timeout, func() {
			expired.Store(true)
			w.Perform(system.ActionClose)
		})
		defer timer.Stop()
	}
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))
	th.Palette.Bg, th.Palette.Fg, th.Palette.ContrastBg = theme.Background, theme.Ink, theme.Accent
	th.Palette.ContrastFg = theme.ButtonInk
	th.TextSize = 14
	var ops op.Ops
	var accept, cancel, reject widget.Clickable
	var secret, repeat widget.Editor
	for _, ed := range []*widget.Editor{&secret, &repeat} {
		ed.SingleLine, ed.Submit, ed.Mask, ed.InputHint = true, true, '•', key.HintPassword
	}
	defer func() { secret = widget.Editor{}; repeat = widget.Editor{} }()
	list := widget.List{List: layout.List{Axis: layout.Vertical}}
	result := response{Code: errCanceled}
	finished, focused := false, false
	errorMessage := p.Error
	finish := func(r response) {
		if !finished {
			finished, result = true, r
			w.Perform(system.ActionClose)
		}
	}
	for {
		switch e := w.Event().(type) {
		case app.DestroyEvent:
			if e.Err != nil {
				return response{Code: errNoPinentry}
			}
			if expired.Load() {
				return response{Code: errTimeout}
			}
			return result
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			if finished {
				e.Frame(gtx.Ops)
				continue
			}
			for {
				ev, ok := gtx.Event(key.Filter{Name: key.NameEscape})
				if !ok {
					break
				}
				if ev.(key.Event).State == key.Press {
					finish(response{Code: errCanceled})
				}
			}
			submit := accept.Clicked(gtx)
			if p.Kind == "GETPIN" {
				for _, ed := range []*widget.Editor{&secret, &repeat} {
					// Consume copy/cut shortcuts before Editor.Update
					for {
						_, ok := gtx.Event(
							key.Filter{Focus: ed, Name: "C", Required: key.ModShortcut},
							key.Filter{Focus: ed, Name: "X", Required: key.ModShortcut})
						if !ok {
							break
						}
					}
					for {
						ev, ok := ed.Update(gtx)
						if !ok {
							break
						}
						if _, ok := ev.(widget.SubmitEvent); ok {
							if ed == &secret && p.RepeatEnabled {
								gtx.Execute(key.FocusCmd{Tag: &repeat})
							} else {
								submit = true
							}
						}
					}
				}
			}
			if submit {
				if p.Kind == "GETPIN" && p.RepeatEnabled && secret.Text() != repeat.Text() {
					errorMessage = p.RepeatError
					if errorMessage == "" {
						errorMessage = "Passphrases do not match. Try again."
					}
					gtx.Execute(key.FocusCmd{Tag: &repeat})
				} else {
					finish(response{Secret: secret.Text()})
				}
			}
			if cancel.Clicked(gtx) {
				code := errCanceled
				if p.Kind == "CONFIRM" && p.NotOK == "" {
					code = errNotConfirmed
				}
				finish(response{Code: code})
			}
			if reject.Clicked(gtx) {
				finish(response{Code: errNotConfirmed})
			}
			paint.Fill(gtx.Ops, theme.Background)
			layout.UniformInset(28).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						label := material.Caption(th, "GPG  /  AUTHENTICATION")
						label.Color = theme.Accent
						return label.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Height: 10}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						heading := p.Title
						if heading == "" {
							heading = "Unlock your key"
							if p.Kind != "GETPIN" {
								heading = "Confirmation required"
								if p.OneButton {
									heading = "Message from GPG"
								}
							}
						}
						label := material.H6(th, heading)
						label.MaxLines = 2
						return label.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Height: 18}.Layout),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						items := []layout.Widget{func(gtx layout.Context) layout.Dimensions {
							label := material.Body2(th, p.Description)
							label.Color = theme.Muted
							return layout.Inset{Bottom: 20}.Layout(gtx, label.Layout)
						}}
						if p.Kind == "GETPIN" {
							items = append(items, passwordField(th, &secret, p.Label))
							if p.RepeatEnabled {
								label := p.Repeat
								if label == "" {
									label = "Repeat passphrase"
								}
								items = append(items, passwordField(th, &repeat, label))
							}
							if errorMessage != "" {
								items = append(items, func(gtx layout.Context) layout.Dimensions {
									label := material.Body2(th, errorMessage)
									label.Color = theme.Danger
									return label.Layout(gtx)
								})
							}
						}
						return material.List(th, &list).Layout(gtx, len(items), func(gtx layout.Context, i int) layout.Dimensions {
							return items[i](gtx)
						})
					}),
					layout.Rigid(layout.Spacer{Height: 16}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						buttons := []layout.FlexChild{}
						if !p.OneButton {
							buttons = append(buttons, layout.Rigid(material.Button(th, &cancel, stripMnemonic(p.Cancel)).Layout), layout.Rigid(layout.Spacer{Width: 8}.Layout))
							if p.Kind == "CONFIRM" && p.NotOK != "" {
								buttons = append(buttons, layout.Rigid(material.Button(th, &reject, stripMnemonic(p.NotOK)).Layout), layout.Rigid(layout.Spacer{Width: 8}.Layout))
							}
						}
						buttons = append(buttons, layout.Rigid(material.Button(th, &accept, stripMnemonic(p.OK)).Layout))
						return layout.Flex{Spacing: layout.SpaceStart, Alignment: layout.Middle}.Layout(gtx, buttons...)
					}),
				)
			})
			if !focused {
				if p.Kind == "GETPIN" {
					gtx.Execute(key.FocusCmd{Tag: &secret})
				} else {
					gtx.Execute(key.FocusCmd{Tag: &accept})
				}
				focused = true
			}
			e.Frame(gtx.Ops)
		}
	}
}

func passwordField(th *material.Theme, ed *widget.Editor, label string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Bottom: 14}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(material.Body2(th, stripMnemonic(label)).Layout),
				layout.Rigid(layout.Spacer{Height: 7}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					border := theme.Muted
					if gtx.Focused(ed) {
						border = theme.Accent
					}
					return widget.Border{Color: border, CornerRadius: 6, Width: 1}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.UniformInset(12).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							gtx.Constraints.Min = image.Pt(gtx.Constraints.Max.X, 0)
							return material.Editor(th, ed, "").Layout(gtx)
						})
					})
				}),
			)
		})
	}
}

// GPG marks keyboard accelerators with underscores; this UI uses Tab/Enter.
func stripMnemonic(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '_' {
			i++
			if i == len(s) {
				break
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
