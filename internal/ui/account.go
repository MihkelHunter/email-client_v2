package ui

import (
	"context"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/MihkelHunter/email-client_v2/internal/config"
	"github.com/MihkelHunter/email-client_v2/internal/gmailauth"
	"github.com/MihkelHunter/email-client_v2/internal/recipients"
)

const accountHelp = `credentials.json is the OAuth client you download from Google Cloud Console ` +
	`(APIs & Services → Credentials). It must be of type "Desktop app" and the Gmail API must be enabled. ` +
	`Logging in opens your browser; the resulting token is stored next to credentials.json.

The From address must be the Gmail account you log in with, or one of its "Send mail as" aliases.`

type accountView struct {
	a       *App
	content fyne.CanvasObject

	credentials *widget.Entry
	tokenLabel  *widget.Label
	loginStatus *widget.Label
	encoding    *widget.Select
	delimiter   *widget.Select
}

func newAccountView(a *App) *accountView {
	s := a.settings
	v := &accountView{a: a}

	v.credentials = widget.NewEntry()
	v.credentials.SetText(s.CredentialsPath)
	v.credentials.OnChanged = func(string) { v.refresh() }
	browse := widget.NewButtonWithIcon("Browse", theme.FolderOpenIcon(), func() {
		a.pickFile(v.credentials.Text, []string{".json"}, func(p string) { v.credentials.SetText(p) })
	})

	v.tokenLabel = widget.NewLabel("")
	v.tokenLabel.Truncation = fyne.TextTruncateEllipsis
	v.loginStatus = widget.NewLabel("")
	loginBtn := widget.NewButtonWithIcon("Log in with Google", theme.LoginIcon(), v.login)
	loginBtn.Importance = widget.HighImportance
	logoutBtn := widget.NewButtonWithIcon("Log out", theme.LogoutIcon(), v.logout)

	v.encoding = widget.NewSelect(recipients.Encodings, nil)
	v.encoding.SetSelected(orDefault(s.CSVEncoding, "auto"))
	v.delimiter = widget.NewSelect(recipients.Delimiters, nil)
	v.delimiter.SetSelected(orDefault(s.CSVDelimiter, "auto"))

	settingsPath, _ := config.Path()
	help := widget.NewLabel(accountHelp)
	help.Wrapping = fyne.TextWrapWord

	v.content = container.NewVScroll(container.NewVBox(
		widget.NewCard("Gmail account", "", container.NewVBox(
			widget.NewForm(
				widget.NewFormItem("credentials.json", pathRow(v.credentials, browse)),
				widget.NewFormItem("Token file", v.tokenLabel),
				widget.NewFormItem("Status", v.loginStatus),
			),
			container.NewHBox(loginBtn, logoutBtn),
			help,
		)),
		widget.NewCard("Recipients CSV", "", widget.NewForm(
			widget.NewFormItem("Encoding", v.encoding),
			widget.NewFormItem("Delimiter", v.delimiter),
			widget.NewFormItem("", widget.NewLabel("\"auto\" uses UTF-8 if the file is valid UTF-8, otherwise Windows-1257 (Excel's Estonian default).")),
		)),
		widget.NewCard("Settings", "", widget.NewLabel("Your last-used values are saved on exit to:\n"+settingsPath)),
	))
	v.refresh()
	return v
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func (v *accountView) collect(s *config.Settings) {
	s.CredentialsPath = strings.TrimSpace(v.credentials.Text)
	s.CSVEncoding = v.encoding.Selected
	s.CSVDelimiter = v.delimiter.Selected
}

// refresh shows whether a token file exists. It does not contact Google.
func (v *accountView) refresh() {
	v.a.settings.CredentialsPath = strings.TrimSpace(v.credentials.Text)
	tok := v.a.settings.TokenFile()
	v.tokenLabel.SetText(tok)
	_, err := os.Stat(tok)
	switch {
	case err == nil:
		v.loginStatus.SetText("Logged in (token saved)")
	case errors.Is(err, fs.ErrNotExist):
		v.loginStatus.SetText("Not logged in")
	default:
		v.loginStatus.SetText(err.Error())
	}
}

func (v *accountView) login() {
	v.a.collectSettings()
	s := v.a.settings
	cfg, err := gmailauth.LoadConfig(s.CredentialsPath)
	if err != nil {
		dialog.ShowError(err, v.a.win)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	urlEntry := widget.NewEntry()
	urlEntry.Disable()
	wait := dialog.NewCustom("Logging in", "Cancel", container.NewVBox(
		widget.NewLabel("Finish the login in your browser.\nIf no browser window opened, copy this address into your browser:"),
		urlEntry,
	), v.a.win)
	wait.SetOnClosed(cancel)
	wait.Resize(fyne.NewSize(600, 200))
	wait.Show()

	go func() {
		tok, err := gmailauth.Login(ctx, cfg, func(authURL string) error {
			u, err := url.Parse(authURL)
			if err != nil {
				return err
			}
			fyne.Do(func() {
				urlEntry.SetText(authURL)
				v.a.fyne.OpenURL(u)
			})
			return nil
		})
		if err == nil {
			err = gmailauth.SaveToken(s.TokenFile(), tok)
		}
		fyne.Do(func() {
			wait.Hide()
			v.refresh()
			switch {
			case errors.Is(err, context.Canceled):
			case errors.Is(err, context.DeadlineExceeded):
				dialog.ShowError(errors.New("login timed out"), v.a.win)
			case err != nil:
				dialog.ShowError(err, v.a.win)
			default:
				dialog.ShowInformation("Logged in", "Login successful. You can now send emails.", v.a.win)
			}
		})
	}()
}

func (v *accountView) logout() {
	tok := v.a.settings.TokenFile()
	dialog.ShowConfirm("Log out", "Delete the saved login token?\n"+tok, func(ok bool) {
		if !ok {
			return
		}
		if err := os.Remove(tok); err != nil && !errors.Is(err, fs.ErrNotExist) {
			dialog.ShowError(err, v.a.win)
		}
		v.refresh()
	}, v.a.win)
}
