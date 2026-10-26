// Package ui is the Fyne desktop interface.
package ui

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"google.golang.org/api/gmail/v1"

	"github.com/MihkelHunter/email-client_v2/internal/config"
	"github.com/MihkelHunter/email-client_v2/internal/gmailauth"
)

type App struct {
	fyne     fyne.App
	win      fyne.Window
	settings config.Settings

	tabs        *container.AppTabs
	campaignTab *container.TabItem
	editorTab   *container.TabItem

	campaign *campaignView
	editor   *editorView
	account  *accountView
}

func Run() {
	a := &App{fyne: app.NewWithID("com.github.mihkelhunter.emailclient")}
	a.win = a.fyne.NewWindow("Email Client")

	settings, err := config.Load()
	a.settings = settings

	a.campaign = newCampaignView(a)
	a.editor = newEditorView(a)
	a.account = newAccountView(a)

	a.campaignTab = container.NewTabItemWithIcon("Email", theme.MailSendIcon(), a.campaign.content)
	a.editorTab = container.NewTabItemWithIcon("Template editor", theme.DocumentCreateIcon(), a.editor.content)
	a.tabs = container.NewAppTabs(
		a.campaignTab,
		a.editorTab,
		container.NewTabItemWithIcon("Account & settings", theme.AccountIcon(), a.account.content),
	)
	a.tabs.OnSelected = func(t *container.TabItem) {
		if t == a.editorTab {
			a.editor.refreshPlaceholders()
		}
	}
	a.win.SetContent(a.tabs)
	a.win.Resize(fyne.NewSize(1100, 750))
	a.win.SetCloseIntercept(a.onClose)

	if err != nil {
		dialog.ShowError(err, a.win)
	}
	if a.settings.RecipientsPath != "" {
		a.campaign.check()
	}
	a.win.ShowAndRun()
}

// collectSettings copies the current widget values into a.settings.
func (a *App) collectSettings() {
	a.campaign.collect(&a.settings)
	a.account.collect(&a.settings)
}

func (a *App) saveSettings() {
	a.collectSettings()
	if err := a.settings.Save(); err != nil {
		fyne.LogError("save settings", err)
	}
}

func (a *App) onClose() {
	closeNow := func() {
		a.saveSettings()
		a.win.Close()
	}
	var reasons []string
	if a.campaign.sending() {
		reasons = append(reasons, "Emails are still being sent; closing stops sending.")
	}
	if a.editor.dirty {
		reasons = append(reasons, "The template has unsaved changes.")
	}
	if len(reasons) == 0 {
		closeNow()
		return
	}
	dialog.ShowConfirm("Quit?", strings.Join(reasons, "\n")+"\n\nQuit anyway?", func(ok bool) {
		if ok {
			a.campaign.stop()
			closeNow()
		}
	}, a.win)
}

func (a *App) gmailService() (*gmail.Service, error) {
	cfg, err := gmailauth.LoadConfig(a.settings.CredentialsPath)
	if err != nil {
		return nil, err
	}
	return gmailauth.NewService(context.Background(), cfg, a.settings.TokenFile())
}

// openInBrowser writes html to a temp file and opens it in the default browser.
func (a *App) openInBrowser(html string) {
	path := filepath.Join(os.TempDir(), "email-client-v2-preview.html")
	if err := os.WriteFile(path, []byte(html), 0o600); err != nil {
		dialog.ShowError(err, a.win)
		return
	}
	u := &url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(path)}
	if err := a.fyne.OpenURL(u); err != nil {
		dialog.ShowError(err, a.win)
	}
}

// pickFile shows an open dialog starting in the folder of current.
func (a *App) pickFile(current string, exts []string, onPicked func(path string)) {
	d := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
		if err != nil {
			dialog.ShowError(err, a.win)
			return
		}
		if r == nil {
			return
		}
		r.Close()
		onPicked(filepath.FromSlash(r.URI().Path()))
	}, a.win)
	if len(exts) > 0 {
		d.SetFilter(storage.NewExtensionFileFilter(exts))
	}
	setDialogLocation(d, current)
	d.Resize(fyne.NewSize(900, 600))
	d.Show()
}

func setDialogLocation(d *dialog.FileDialog, current string) {
	dir := current
	if dir == "" {
		dir, _ = os.Getwd()
	} else if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		dir = filepath.Dir(dir)
	}
	if l, err := storage.ListerForURI(storage.NewFileURI(dir)); err == nil {
		d.SetLocation(l)
	}
}

// pathRow is an entry with buttons to its right.
func pathRow(entry *widget.Entry, buttons ...fyne.CanvasObject) fyne.CanvasObject {
	return container.NewBorder(nil, nil, nil, container.NewHBox(buttons...), entry)
}
