package main

import (
	"context"
	"runtime/debug"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotkit/app"
)

// NewAboutDialog creates a new About dialog for CamWatch.
func NewAboutDialog(ctx context.Context) *gtk.AboutDialog {
	d := gtk.NewAboutDialog()
	d.SetApplication(app.FromContext(ctx).Application)
	d.SetTransientFor(app.GTKWindowFromContext(ctx))
	d.SetModal(true)

	d.SetLogoIconName("camera-web")
	d.SetProgramName("CamWatch")
	d.SetAuthors([]string{
		"diamondburned",
	})
	d.AddCreditSection("Made specifically for", []string{
		"CSUF OSS Club",
	})
	d.SetLicenseType(gtk.LicenseGPL30)
	d.SetWebsite("https://github.com/diamondburned/camwatch")
	d.SetWebsiteLabel("Source Code")

	if buildInfo, ok := debug.ReadBuildInfo(); ok {
		d.SetVersion(buildInfoVersion(buildInfo))

		deps := make([]string, len(buildInfo.Deps))
		for i, dep := range buildInfo.Deps {
			deps[i] = dep.Path
		}
		d.AddCreditSection("Dependencies", deps)
	}

	return d
}

func buildInfoVersion(info *debug.BuildInfo) string {
	if info.Main.Version != "(devel)" {
		return info.Main.Version
	}

	vcs := searchBuildSettings(info.Settings, "vcs")
	rev := searchBuildSettings(info.Settings, "vcs.revision")
	if vcs != "" && rev != "" {
		if len(rev) > 7 {
			rev = rev[:7]
		}
		return vcs + "-" + rev
	}
	if vcs != "" {
		return vcs + "-devel"
	}

	return "(devel)"
}

func searchBuildSettings(settings []debug.BuildSetting, k string) string {
	for _, setting := range settings {
		if setting.Key == k {
			return setting.Value
		}
	}
	return ""
}
