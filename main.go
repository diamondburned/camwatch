package main

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"runtime"
	"time"

	"github.com/diamondburned/gotk4/pkg/core/gioutil"
	"github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdkpixbuf/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotkit/app"
	"github.com/diamondburned/gotkit/app/prefs/kvstate"
	"github.com/diamondburned/gotkit/gtkutil"
	"github.com/diamondburned/gotkit/gtkutil/cssutil"
	"github.com/diamondburned/gotkit/gtkutil/imgutil"
	"github.com/pkg/errors"
)

func main() {
	app := app.New("com.github.diamondburned.camwatch", "CamWatch")
	app.ConnectActivate(func() { activate(app.Context()) })
	app.RunMain(context.Background())
}

func activate(ctx context.Context) {
	win := app.FromContext(ctx).NewWindow()
	win.NewWindowHandle() // clear this out
	win.SetDefaultSize(960, 540)
	win.SetTitle("")

	ctx = app.WithWindow(ctx, win)

	configs := NewConfigPage()
	configs.RestoreConfig(kvstate.AcquireConfig(ctx, "cameracfg"))

	pages := gtk.NewStack()
	pages.SetTransitionType(gtk.StackTransitionTypeCrossfade)
	pages.AddChild(configs)

	var currentCam *CameraView

	configs.ConnectActivate(func() {
		cam := NewCameraView(ctx, configs.URL(), configs.FPS())
		cam.ConnectBack(func() {
			pages.SetVisibleChild(configs)
		})

		pages.AddChild(cam)
		pages.SetVisibleChild(cam)

		if currentCam != nil {
			pages.Remove(currentCam)
		}
		currentCam = cam
	})

	win.SetChild(pages)
	win.Show()
}

type ConfigPage struct {
	*gtk.Box
	url     *gtk.Entry
	fps     *gtk.SpinButton
	connect *gtk.Button
}

func NewConfigPage() *ConfigPage {
	connect := gtk.NewButtonWithLabel("Connect")
	connect.AddCSSClass("suggested")
	connect.SetHasFrame(true)

	url := gtk.NewEntry()
	url.SetInputPurpose(gtk.InputPurposeURL)
	url.SetActivatesDefault(true)
	url.SetPlaceholderText("http://localhost/snap.jpeg")
	url.SetHExpand(true)
	url.ConnectActivate(func() { connect.Activate() })

	fps := gtk.NewSpinButtonWithRange(1, 60, 1)
	fps.SetValue(15)

	topBox := gtk.NewBox(gtk.OrientationHorizontal, 6)
	topBox.Append(url)
	topBox.Append(fps)

	midBox := gtk.NewBox(gtk.OrientationVertical, 6)
	midBox.SetVExpand(true)
	midBox.SetHExpand(true)
	midBox.SetVAlign(gtk.AlignCenter)
	midBox.SetHAlign(gtk.AlignCenter)
	midBox.Append(topBox)
	midBox.Append(connect)

	header := gtk.NewHeaderBar()
	header.AddCSSClass("titlebar")

	box := gtk.NewBox(gtk.OrientationVertical, 0)
	box.Append(header)
	box.Append(midBox)

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

type CameraHeader struct {
	*gtk.CenterBox
	b *gtk.Button
	t *gtk.Label

	past time.Time
}

var headerCSS = cssutil.Applier("camera-header", `
	.titlebar {
		min-height: 0;
	}
	.camera-header {
		padding: 4px;
		opacity: 0.25;
		transition: linear 75ms all;
	}
	.camera-header:not(:hover),
	.camera-header button:not(:hover) {
		background: none;
		box-shadow: none;
		outline: none;
		border-color: transparent;
	}
	.camera-header:hover {
		opacity: 1;
	}
	.camera-header button {
		transition: linear 75ms all;
	}
	.camera-header > *:last-child > windowcontrols {
		margin-left: 4px;
	}
	.camera-time {
		font-family: monospace;
		font-size: 0.9em;
		margin-right: 0.5em;
	}
`)

func NewCameraHeader(ctx context.Context) *CameraHeader {
	h := CameraHeader{}
	h.t = gtk.NewLabel("")
	h.t.AddCSSClass("camera-time")
	h.t.Hide()
	h.t.SetHAlign(gtk.AlignEnd)

	h.b = gtk.NewButtonFromIconName("go-previous-symbolic")

	fullscreen := gtk.NewToggleButton()
	fullscreen.SetIconName("view-fullscreen-symbolic")

	win := app.WindowFromContext(ctx)
	win.NotifyProperty("fullscreened", func() {
		isFullscreen := win.IsFullscreen()
		fullscreen.SetActive(isFullscreen)
	})

	fullscreen.ConnectToggled(func() {
		w := app.WindowFromContext(ctx)
		if fullscreen.Active() {
			w.Fullscreen()
		} else {
			w.Unfullscreen()
		}
	})

	end := gtk.NewBox(gtk.OrientationHorizontal, 0)
	end.Append(h.t)
	end.Append(fullscreen)
	end.Append(gtk.NewWindowControls(gtk.PackEnd))

	start := gtk.NewBox(gtk.OrientationHorizontal, 0)
	start.Append(gtk.NewWindowControls(gtk.PackStart))
	start.Append(h.b)

	h.CenterBox = gtk.NewCenterBox()
	h.AddCSSClass("titlebar")
	h.SetStartWidget(start)
	h.SetEndWidget(end)
	headerCSS(h)

	return &h
}

// SetVisibleTime sets the visible time. If t is zero, then the time is hidden.
func (h *CameraHeader) SetVisibleTime(t time.Time) {
	defer func() { h.past = t }()

	if t.IsZero() {
		h.t.Hide()
		return
	}

	msg := t.Format("3:04:05.000 PM")
	if !h.past.IsZero() {
		msg += " " + shortDura(t.Sub(h.past))
	}

	h.t.Show()
	h.t.SetText(msg)
}

func shortDura(d time.Duration) string {
	if d >= time.Second {
		return fmt.Sprintf("%.3fs ", d.Seconds())
	}
	return fmt.Sprintf("0.%03dms", d.Milliseconds())
}

type CameraView struct {
	*gtk.WindowHandle
	overlay *gtk.Overlay
	picture *gtk.Picture
	header  *CameraHeader
	error   *gtk.Label

	ctx gtkutil.Cancellable
}

var cameraViewCSS = cssutil.Applier("camera-view", ``)

func NewCameraView(ctx context.Context, url string, fps int) *CameraView {
	picture := gtk.NewPicture()
	picture.SetCanShrink(true)
	picture.SetVExpand(true)
	picture.SetHExpand(true)

	error := gtk.NewLabel("")
	error.AddCSSClass("camera-error")
	error.Hide()
	error.SetXAlign(0.5)
	error.SetYAlign(0.5)

	header := NewCameraHeader(ctx)
	header.SetVAlign(gtk.AlignStart)

	picture.NotifyProperty("paintable", func() {
		header.SetVisibleTime(time.Now())
	})

	overlay := gtk.NewOverlay()
	overlay.AddOverlay(error)
	overlay.AddOverlay(header)
	overlay.SetChild(picture)

	handle := gtk.NewWindowHandle()
	handle.SetChild(overlay)
	cameraViewCSS(header)

	cam := CameraView{
		WindowHandle: handle,
		overlay:      overlay,
		header:       header,
		picture:      picture,
		error:        error,
	}

	cam.ctx = gtkutil.WithVisibility(ctx, cam)
	cam.ctx.OnRenew(func(ctx context.Context) func() {
		cam.start(ctx, url, fps)
		return func() {}
	})

	return &cam
}

func (cam *CameraView) start(ctx context.Context, url string, fps int) {
	ticker := time.NewTicker(time.Second / time.Duration(fps))

	go func() {
		defer ticker.Stop()

		var f frameDownloader
		var lastPixbuf *gdkpixbuf.Pixbuf

		for {
			pixbuf, err := f.download(ctx, url)

			if lastPixbuf == pixbuf {
				goto wait
			}
			lastPixbuf = pixbuf

			glib.IdleAdd(func() {
				cam.error.SetVisible(err != nil)

				if err != nil {
					cam.error.SetText("Error: " + err.Error())
					return
				}

				alloc := cam.picture.Allocation()
				scale := cam.picture.ScaleFactor()

				w := alloc.Width() * scale
				h := alloc.Height() * scale

				w, h = imgutil.MaxSize(pixbuf.Width(), pixbuf.Height(), w, h)
				pixbuf = pixbuf.ScaleSimple(w, h, gdkpixbuf.InterpBilinear)

				cam.picture.SetPixbuf(pixbuf)
			})

		wait:
			runtime.GC()

			select {
			case <-ticker.C:
				continue
			case <-ctx.Done():
				return
			}
		}
	}()
}

// ConnectBack connects f to be called when the user hits the back button.
func (cam *CameraView) ConnectBack(f func()) {
	cam.header.b.ConnectClicked(f)
}

type frameDownloader struct {
	pixbuf *gdkpixbuf.Pixbuf
	etag   string
}

func (f *frameDownloader) download(ctx context.Context, url string) (*gdkpixbuf.Pixbuf, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, errors.Wrap(err, "cannot create GET request")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "cannot do GET request")
	}
	defer resp.Body.Close()

	etag := resp.Header.Get("ETag")
	if etag != "" && etag == f.etag {
		return f.pixbuf, nil
	}

	loader := gdkpixbuf.NewPixbufLoader()
	defer loader.Close()

	if _, err := io.Copy(gioutil.PixbufLoaderWriter(loader), resp.Body); err != nil {
		return nil, errors.Wrap(err, "cannot download frame")
	}

	if err := loader.Close(); err != nil {
		return nil, errors.Wrap(err, "cannot finalize frame")
	}

	pixbuf := loader.Pixbuf()
	if etag != "" {
		f.etag = etag
		f.pixbuf = pixbuf
	}

	return pixbuf, nil
}
