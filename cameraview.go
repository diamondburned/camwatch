package main

import (
	"context"
	"fmt"
	"image/color"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/diamondburned/gotk4/pkg/cairo"
	"github.com/diamondburned/gotk4/pkg/core/gioutil"
	"github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdkpixbuf/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotkit/app"
	"github.com/diamondburned/gotkit/app/prefs"
	"github.com/diamondburned/gotkit/gtkutil"
	"github.com/diamondburned/gotkit/gtkutil/cssutil"
	"github.com/diamondburned/gotkit/gtkutil/imgutil"
	"github.com/diamondburned/vgcairo"
	"github.com/pkg/errors"
	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
	"gonum.org/v1/plot/vg/draw"
)

type CameraHeader struct {
	*gtk.CenterBox
	b *gtk.Button
	t *gtk.Label
	g *LatencyGraph

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
	.camera-header > *:last-child > *:not(:first-child):not(.camera-time) {
		margin-left: 4px;
	}
	.camera-time {
		font-family: monospace;
		font-size: 0.9em;
		margin-left: 8px;
	}
`)

func NewCameraHeader(ctx context.Context) *CameraHeader {
	h := CameraHeader{}
	h.g = NewLatencyGraph(ctx, 12)
	h.g.Hide()
	h.g.SetVAlign(gtk.AlignCenter)

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

	menu := gtk.NewButtonFromIconName("open-menu-symbolic")
	menu.ConnectClicked(func() {
		gtkutil.ShowPopoverMenu(menu, gtk.PosBottom, [][2]string{
			{"_Preferences", "win.preferences"},
			{"_Quit", "app.quit"},
		})
	})

	end := gtk.NewBox(gtk.OrientationHorizontal, 0)
	end.Append(h.g)
	end.Append(h.t)
	end.Append(fullscreen)
	end.Append(menu)
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

var timeFormat = prefs.NewString("3:04:05.000 PM", prefs.StringMeta{
	Name:        "Time Format",
	Section:     "Header",
	Description: "The format of the timestamp at the top-right corner. See https://pkg.go.dev/time#pkg-constants.",
	Placeholder: "3:04:05.000 PM",
})

// SetVisibleTime sets the visible time. If t is zero, then the time is hidden.
func (h *CameraHeader) SetVisibleTime(t time.Time) {
	defer func() { h.past = t }()

	if t.IsZero() {
		h.t.Hide()
		return
	}

	msg := t.Format(timeFormat.Value())
	if !h.past.IsZero() {
		latency := t.Sub(h.past)
		msg += " " + shortDura(latency)

		h.g.Show()
		h.g.AddValue(latency.Seconds())
	}

	h.t.Show()
	h.t.SetText(msg)
}

func shortDura(d time.Duration) string {
	ms := d.Milliseconds()
	if ms > 9999 {
		return fmt.Sprintf("%.3gs ", d.Seconds())
	}
	return fmt.Sprintf("%4dms", d.Milliseconds())
}

// LatencyGraph is a mini sparkline-style graph showing the time between
// fetches. It isn't latency per-se, but since the graph automatically scales,
// it doesn't matter.
type LatencyGraph struct {
	*gtk.DrawingArea
	plot *plot.Plot
	line *plotter.Line
	max  int
}

var latencyGraphCSS = cssutil.Applier("latency-graph", `
	.latency-graph.graph-line {
		background: linear-gradient(to top, alpha(green, 0.65), alpha(lime, 0.85));
	}
`)

func NewLatencyGraph(ctx context.Context, max int) *LatencyGraph {
	g := LatencyGraph{max: max}

	g.line = &plotter.Line{
		XYs: make(plotter.XYs, 0, max),
		LineStyle: draw.LineStyle{
			Color: color.Transparent,
			Width: vg.Points(2),
		},
	}

	g.plot = plot.New()
	g.plot.X.Padding = 0
	g.plot.X.Min = 0
	g.plot.X.Max = float64(max - 1)
	g.plot.Y.Padding = vg.Length(0)
	g.plot.BackgroundColor = color.Transparent
	g.plot.HideAxes()
	g.plot.Add(g.line)

	g.DrawingArea = gtk.NewDrawingArea()
	g.DrawingArea.SetSizeRequest(64, 18)
	latencyGraphCSS(g.DrawingArea)

	var bgsurface *cairo.Surface

	g.SetDrawFunc(func(_ *gtk.DrawingArea, t *cairo.Context, w, h int) {
		if bgsurface == nil || bgsurface.Width() != w || bgsurface.Height() != h {
			bgsurface = t.Target().CreateSimilar(cairo.ContentColorAlpha, w, h)
			// Hack this by setting the line color to a value that only vgcairo
			// understands.
			g.line.LineStyle.Color = vgcairo.ColorSurface(bgsurface, 0, 0)

			styles := g.StyleContext()
			styles.Save()
			defer styles.Restore()

			styles.AddClass("graph-line")
			gtk.RenderBackground(styles, cairo.Create(bgsurface), 0, 0, float64(w), float64(h))
		}

		t.SetLineCap(cairo.LineCapRound)

		g.plot.Draw(draw.NewCanvas(
			vgcairo.NewCanvas(t),
			vg.Length(float64(w)),
			vg.Length(float64(h)),
		))
	})

	return &g
}

// AddValue adds a value into the lqtency graph.
func (g *LatencyGraph) AddValue(val float64) {
	// (0, 0) is actually at the top-left corner, so we do this.
	val = -val

	if len(g.line.XYs) == g.max {
		// Shift leftwards once to make space for the last entry.
		copy(g.line.XYs, g.line.XYs[1:])
		g.line.XYs[len(g.line.XYs)-1].Y = val
	} else {
		g.line.XYs = append(g.line.XYs, plotter.XY{Y: val})
	}

	yMin := math.Inf(+1)
	yMax := math.Inf(-1)
	for _, pt := range g.line.XYs {
		yMin = math.Min(yMin, pt.Y)
		yMax = math.Max(yMax, pt.Y)
	}

	// Flip because we negated val.
	g.plot.Y.Min = yMax
	g.plot.Y.Max = yMin

	x := float64(g.max)
	for i := len(g.line.XYs) - 1; i >= 0; i-- {
		x--
		g.line.XYs[i].X = x
	}

	g.QueueDraw()
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

var denoiseThreshold = prefs.NewInt(0, prefs.IntMeta{
	Name:        "Denoise Threshold",
	Section:     "Video",
	Description: "Denoise strength, or 0 to not denoise at all.",
	Min:         0,
	Max:         500,
})

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

	if denoiseThresh := denoiseThreshold.Value(); denoiseThresh > 0 {
		ffmpeg := exec.CommandContext(ctx, "ffmpeg",
			"-loglevel", "warning",
			"-i", "-",
			"-c:v", "mjpeg", "-qscale:v", "2",
			"-vf", fmt.Sprintf("vaguedenoiser=threshold=%d", denoiseThresh),
			"-f", "image2pipe", "-",
		)

		ffmpeg.Stdin = resp.Body
		ffmpeg.Stderr = os.Stderr
		ffmpeg.Stdout = gioutil.PixbufLoaderWriter(loader)

		if err := ffmpeg.Run(); err != nil {
			return nil, errors.Wrap(err, "cannot download frame over ffmpeg")
		}
	} else {
		if _, err := io.Copy(gioutil.PixbufLoaderWriter(loader), resp.Body); err != nil {
			return nil, errors.Wrap(err, "cannot download frame")
		}
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
