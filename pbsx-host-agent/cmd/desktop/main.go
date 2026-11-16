//go:build desktop

// PBS-X Desktop is a native companion window; the background service owns backups.
package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/pbs-x/pbsx-host-agent/internal/agentstatus"
	"github.com/pbs-x/pbsx-host-agent/internal/desktopcontrol"
)

var Version = "0.7.0-preview"

func bytesText(value uint64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	n := float64(value)
	index := 0
	for n >= 1024 && index < len(units)-1 {
		n /= 1024
		index++
	}
	return fmt.Sprintf("%.1f %s", n, units[index])
}

func main() {
	demo := flag.Bool("demo", false, "Show an offline preview with all administrative actions disabled")
	version := flag.Bool("version", false, "Print the desktop version")
	flag.Parse()
	if *version {
		fmt.Println(Version)
		return
	}
	a := app.NewWithID("com.pbsx.desktop")
	a.Settings().SetTheme(theme.DarkTheme())
	window := a.NewWindow("PBS-X Agent")
	window.Resize(fyne.NewSize(880, 600))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	window.SetOnClosed(cancel)
	headline := widget.NewLabelWithStyle("Reading agent status…", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	versionLabel := widget.NewLabel("Agent version: —")
	heartbeat := widget.NewLabel("Last successful heartbeat: —")
	operation := widget.NewLabel("No operation reported")
	operation.Wrapping = fyne.TextWrapWord
	lastRun := widget.NewLabel("No completed run reported")
	progress := widget.NewProgressBarInfinite()
	progress.Stop()
	progress.Hide()
	statusNote := widget.NewLabel("Backups continue when this window is closed.")
	statusNote.Wrapping = fyne.TextWrapWord
	overview := container.NewVBox(widget.NewLabelWithStyle("Your backup agent", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		headline, versionLabel, heartbeat, widget.NewSeparator(), operation, progress, lastRun, widget.NewSeparator(), statusNote)
	code := widget.NewPasswordEntry()
	code.SetPlaceHolder("Paste a PBS-X enrollment code")
	codeAction := widget.NewButton("Connect and install service", nil)
	connectNote := widget.NewLabel("Generate an enrollment code on your PBS-X server, then paste it here. Administrator approval is requested only when applying changes.")
	connectNote.Wrapping = fyne.TextWrapWord
	connect := container.NewVBox(widget.NewLabelWithStyle("Connect this device", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		connectNote, code, codeAction, widget.NewLabel("The code is cleared after submission and is not saved by this application."))
	controller := desktopcontrol.Controller{Elevate: true}
	var busy bool
	var buttons []*widget.Button
	startAction := func(action func(context.Context) error, done string) {
		if busy {
			dialog.ShowInformation("Operation in progress", "Wait for the current administrator operation to finish.", window)
			return
		}
		busy = true
		for _, button := range buttons {
			button.Disable()
		}
		code.Disable()
		go func() {
			operationCtx, stop := context.WithTimeout(ctx, 2*time.Minute)
			defer stop()
			err := action(operationCtx)
			fyne.Do(func() {
				busy = false
				if !*demo {
					for _, button := range buttons {
						button.Enable()
					}
					code.Enable()
				}
				if err != nil {
					dialog.ShowError(err, window)
				} else {
					dialog.ShowInformation("PBS-X Agent", done, window)
				}
			})
		}()
	}
	codeAction.OnTapped = func() {
		value := code.Text
		code.SetText("")
		startAction(func(ctx context.Context) error { return controller.Enroll(ctx, value) }, "Enrollment completed. The background service is running.")
	}
	buttons = append(buttons, codeAction)
	serviceControls := container.NewVBox()
	for _, action := range []string{"start", "stop", "restart"} {
		name := action
		label := map[string]string{"start": "Start agent", "stop": "Stop agent", "restart": "Restart agent"}[name]
		button := widget.NewButton(label, func() {
			perform := func() {
				startAction(func(ctx context.Context) error { return controller.Service(ctx, name) }, "Agent service command completed.")
			}
			if name == "start" {
				perform()
				return
			}
			dialog.ShowConfirm(label, "This can interrupt an active backup or restore. Continue?", func(ok bool) {
				if ok {
					perform()
				}
			}, window)
		})
		buttons = append(buttons, button)
		serviceControls.Add(button)
	}
	serviceNote := widget.NewLabel("The service runs independently of the desktop window. Stop or restart it only when you intend to interrupt its work.")
	serviceNote.Wrapping = fyne.TextWrapWord
	servicePanel := container.NewVBox(widget.NewLabelWithStyle("Background service", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), serviceNote, serviceControls)
	about := widget.NewLabel(fmt.Sprintf("PBS-X Desktop %s\n\nWindows and Linux host backup companion.\n\nHeadless Linux: pbsx-agentctl status --json, watch, enroll --stdin, start, stop and restart.\n\nBackup jobs and restore requests remain managed by your PBS-X server.", Version))
	about.Wrapping = fyne.TextWrapWord
	tabs := container.NewAppTabs(container.NewTabItem("Overview", container.NewPadded(overview)),
		container.NewTabItem("Connect", container.NewPadded(connect)), container.NewTabItem("Service", container.NewPadded(servicePanel)),
		container.NewTabItem("About", container.NewPadded(about)))
	tabs.SetTabLocation(container.TabLocationLeading)
	window.SetContent(tabs)
	if *demo {
		for _, button := range buttons {
			button.Disable()
		}
		code.Disable()
		statusNote.SetText("Offline preview — administrator controls are disabled.")
	}
	go func() {
		tick := time.NewTicker(2 * time.Second)
		defer tick.Stop()
		refresh := func() {
			value, err := agentstatus.Read()
			if *demo {
				value = agentstatus.Snapshot{Version: Version, Phase: "ready", Connected: true, UpdatedAt: time.Now().Unix(), LastHeartbeat: time.Now().Unix(),
					Operation: &agentstatus.Operation{Kind: "backup", Stage: "streaming", Files: 274, ProcessedBytes: 67108864, UploadedBytes: 12582912}}
				err = nil
			}
			fyne.Do(func() {
				if err != nil {
					headline.SetText("Agent status unavailable — use Connect to set up this device")
					progress.Stop()
					progress.Hide()
					return
				}
				running := value.Running(time.Now())
				switch {
				case !running:
					headline.SetText("Agent not running")
				case value.Phase == "waiting-for-enrollment":
					headline.SetText("Waiting for enrollment")
				case value.Connected:
					headline.SetText("Connected to PBS-X")
				default:
					headline.SetText("Agent running — waiting for server contact")
				}
				versionLabel.SetText("Agent version: " + value.Version)
				if value.LastHeartbeat != 0 {
					heartbeat.SetText("Last successful heartbeat: " + time.Unix(value.LastHeartbeat, 0).Local().Format("15:04:05"))
				}
				if value.Operation != nil && running {
					op := value.Operation
					operation.SetText(fmt.Sprintf("%s — %s\n%d files · %s read · %s uploaded", op.Kind, op.Stage, op.Files, bytesText(op.ProcessedBytes), bytesText(op.UploadedBytes)))
					progress.Show()
					progress.Start()
				} else {
					operation.SetText("No backup or restore running")
					progress.Stop()
					progress.Hide()
				}
				if value.LastSuccess != nil {
					if *value.LastSuccess {
						lastRun.SetText("Last operation completed successfully")
					} else {
						lastRun.SetText("Last operation failed — check the agent log")
					}
				}
			})
		}
		refresh()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				refresh()
			}
		}
	}()
	window.ShowAndRun()
}
