package main

import (
	"context"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotkit/app"
	"github.com/diamondburned/gotkit/app/prefs/kvstate"
	"github.com/diamondburned/gotkit/components/prefui"
	"github.com/diamondburned/gotkit/gtkutil"
)

func main() {
	app := app.New("com.github.diamondburned.camwatch", "CamWatch")
	app.AddActions(map[string]func(){
		"app.quit": app.Quit,
	})
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

	gtkutil.BindActionMap(win, map[string]func(){
		"win.preferences": func() { prefui.ShowDialog(ctx) },
		"win.about":       func() { NewAboutDialog(ctx).Show() },
	})

	win.SetChild(pages)
	win.Show()
}
