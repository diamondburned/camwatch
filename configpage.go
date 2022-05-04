package main

import (
	"math"
	"net/url"
	"strings"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotkit/app/prefs/kvstate"
	"github.com/diamondburned/gotkit/gtkutil/cssutil"
)

type ConfigPage struct {
	*gtk.Box
	url     *gtk.Entry
	fps     *gtk.SpinButton
	connect *gtk.Button
}

var configPageCSS = cssutil.Applier("config-page", `
	.config-page-form {
		margin: 8px;
	}
`)

func NewConfigPage() *ConfigPage {
	connect := gtk.NewButtonWithLabel("Connect")
	connect.AddCSSClass("suggested")
	connect.SetHasFrame(true)

	url := gtk.NewEntry()
	url.SetInputPurpose(gtk.InputPurposeURL)
	url.SetActivatesDefault(true)
	url.SetPlaceholderText("http://localhost/snap.jpeg" + strings.Repeat(" ", 60)) // nat size hack
	url.SetHExpand(true)
	url.ConnectActivate(func() { connect.Activate() })

	fps := gtk.NewSpinButtonWithRange(1, 60, 1)
	fps.SetTooltipText("Updates per second")
	fps.SetValue(1)

	topBox := gtk.NewBox(gtk.OrientationHorizontal, 6)
	topBox.Append(url)
	topBox.Append(fps)

	form := gtk.NewBox(gtk.OrientationVertical, 6)
	form.AddCSSClass("config-page-form")
	form.SetVExpand(true)
	form.SetHExpand(true)
	form.SetVAlign(gtk.AlignCenter)
	form.SetHAlign(gtk.AlignCenter)
	form.Append(topBox)
	form.Append(connect)

	header := gtk.NewHeaderBar()
	header.AddCSSClass("titlebar")

	box := gtk.NewBox(gtk.OrientationVertical, 0)
	box.Append(header)
	box.Append(form)
	configPageCSS(box)

	p := ConfigPage{
		Box:     box,
		url:     url,
		fps:     fps,
		connect: connect,
	}

	p.updateButton()
	fps.ConnectChanged(p.updateButton)
	url.ConnectChanged(p.updateButton)

	return &p
}

func (p *ConfigPage) updateButton() {
	p.connect.SetSensitive(p.validate())
}

func (p *ConfigPage) validate() bool {
	ustr := p.URL()
	if ustr == "" {
		return false
	}

	_, err := url.Parse(ustr)
	return err == nil
}

// ConnectActivate connects f to be called when the user hits the Connect
// button.
func (p *ConfigPage) ConnectActivate(f func()) {
	p.connect.ConnectClicked(f)
}

// RestoreConfig restores the input fields to the state inside the given
// kvstate.Config.
func (p *ConfigPage) RestoreConfig(cfg *kvstate.Config) {
	// TODO: make Config struct
	var fps float64
	var url string

	if cfg.Get("fps", &fps) {
		p.fps.SetValue(fps)
	}
	if cfg.Get("url", &url) {
		p.url.SetText(url)
	}

	p.connect.ConnectClicked(func() {
		cfg.Set("fps", p.FPS())
		cfg.Set("url", p.URL())
	})
}

func (p *ConfigPage) FPS() int {
	return int(math.Round(p.fps.Value()))
}

func (p *ConfigPage) URL() string {
	return p.url.Text()
}
