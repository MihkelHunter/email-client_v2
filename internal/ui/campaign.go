package ui

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"google.golang.org/api/gmail/v1"

	"github.com/MihkelHunter/email-client_v2/internal/campaign"
	"github.com/MihkelHunter/email-client_v2/internal/config"
	"github.com/MihkelHunter/email-client_v2/internal/mailer"
	"github.com/MihkelHunter/email-client_v2/internal/recipients"
)

var tableColumns = []struct {
	title string
	width float32
}{
	{"Row", 50},
	{"Email", 260},
	{"Name", 170},
	{"Template", 160},
	{"Status", 420},
}

type rowStatus struct {
	text string
	imp  widget.Importance
}

type campaignView struct {
	a       *App
	content fyne.CanvasObject

	from           *widget.Entry
	subject        *widget.Entry
	recipientsPath *widget.Entry
	templatePath   *widget.Entry
	delay          *widget.Entry
	useCSVTemplate *widget.Check

	table    *widget.Table
	jobs     []campaign.Job
	rows     []rowStatus
	selected int
	headers  []string

	progress *widget.ProgressBar
	status   *widget.Label
	buttons  []*widget.Button
	stopBtn  *widget.Button
	logBtn   *widget.Button

	cancel  context.CancelFunc
	logPath string
}

func newCampaignView(a *App) *campaignView {
	s := a.settings
	c := &campaignView{a: a, selected: -1}

	c.from = widget.NewEntry()
	c.from.SetPlaceHolder("you@gmail.com  or  Your Name <you@gmail.com>")
	c.from.SetText(s.From)

	c.subject = widget.NewEntry()
	c.subject.SetPlaceHolder("Subject – placeholders like {{name}} work here too")
	c.subject.SetText(s.Subject)

	c.recipientsPath = widget.NewEntry()
	c.recipientsPath.SetPlaceHolder("recipients.csv")
	c.recipientsPath.SetText(s.RecipientsPath)

	c.templatePath = widget.NewEntry()
	c.templatePath.SetPlaceHolder("template.html")
	c.templatePath.SetText(s.TemplatePath)

	c.delay = widget.NewEntry()
	c.delay.SetText(strconv.FormatFloat(s.DelaySeconds, 'f', -1, 64))

	c.useCSVTemplate = widget.NewCheck("Use the Template column from the CSV when a row has one (looked up in the template's folder)", nil)
	c.useCSVTemplate.SetChecked(s.UseCSVTemplate)

	browseCSV := widget.NewButtonWithIcon("Browse", theme.FolderOpenIcon(), func() {
		a.pickFile(c.recipientsPath.Text, []string{".csv", ".txt"}, func(p string) {
			c.recipientsPath.SetText(p)
			c.check()
		})
	})
	openCSV := widget.NewButtonWithIcon("Open", theme.FileTextIcon(), func() {
		if c.recipientsPath.Text == "" {
			return
		}
		a.fyne.OpenURL(&url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(c.recipientsPath.Text)})
	})
	browseTemplate := widget.NewButtonWithIcon("Browse", theme.FolderOpenIcon(), func() {
		a.pickFile(c.templatePath.Text, []string{".html", ".htm"}, func(p string) {
			c.templatePath.SetText(p)
		})
	})
	editTemplate := widget.NewButtonWithIcon("Edit", theme.DocumentCreateIcon(), func() {
		if c.templatePath.Text == "" {
			a.tabs.Select(a.editorTab)
			return
		}
		a.editor.open(c.templatePath.Text, func() { a.tabs.Select(a.editorTab) })
	})

	form := widget.NewForm(
		widget.NewFormItem("From", c.from),
		widget.NewFormItem("Subject", c.subject),
		widget.NewFormItem("Recipients CSV", pathRow(c.recipientsPath, browseCSV, openCSV)),
		widget.NewFormItem("Template", pathRow(c.templatePath, browseTemplate, editTemplate)),
		widget.NewFormItem("", c.useCSVTemplate),
		widget.NewFormItem("Delay between emails (s)", container.NewGridWrap(fyne.NewSize(120, c.delay.MinSize().Height), c.delay)),
	)

	checkBtn := widget.NewButtonWithIcon("Load & check", theme.ViewRefreshIcon(), c.check)
	previewBtn := widget.NewButtonWithIcon("Preview selected", theme.VisibilityIcon(), c.preview)
	testBtn := widget.NewButtonWithIcon("Send test to me", theme.MailForwardIcon(), c.sendTest)
	sendBtn := widget.NewButtonWithIcon("Send all", theme.MailSendIcon(), c.sendAll)
	sendBtn.Importance = widget.HighImportance
	c.stopBtn = widget.NewButtonWithIcon("Stop", theme.MediaStopIcon(), c.stop)
	c.stopBtn.Importance = widget.DangerImportance
	c.stopBtn.Disable()
	c.logBtn = widget.NewButtonWithIcon("Open last log", theme.ListIcon(), func() {
		a.fyne.OpenURL(&url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(c.logPath)})
	})
	c.logBtn.Disable()
	c.buttons = []*widget.Button{checkBtn, previewBtn, testBtn, sendBtn, browseCSV, browseTemplate}

	actions := container.NewHBox(checkBtn, previewBtn, testBtn, sendBtn, c.stopBtn, c.logBtn)

	c.table = widget.NewTableWithHeaders(
		func() (int, int) { return len(c.jobs), len(tableColumns) },
		func() fyne.CanvasObject {
			l := widget.NewLabel("")
			l.Truncation = fyne.TextTruncateEllipsis
			return l
		},
		c.updateCell,
	)
	c.table.ShowHeaderColumn = false
	c.table.CreateHeader = func() fyne.CanvasObject {
		return widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	}
	c.table.UpdateHeader = func(id widget.TableCellID, o fyne.CanvasObject) {
		if id.Col >= 0 {
			o.(*widget.Label).SetText(tableColumns[id.Col].title)
		}
	}
	for i, col := range tableColumns {
		c.table.SetColumnWidth(i, col.width)
	}
	c.table.OnSelected = func(id widget.TableCellID) {
		c.selected = id.Row
		if id.Row >= 0 && id.Row < len(c.jobs) {
			j := c.jobs[id.Row]
			c.status.SetText(fmt.Sprintf("Row %d: %s – %s", j.Recipient.Row, j.Recipient.Email, c.rows[id.Row].text))
		}
	}

	c.progress = widget.NewProgressBar()
	c.status = widget.NewLabel("Choose a recipients file and a template, then press \"Load & check\".")
	c.status.Wrapping = fyne.TextWrapWord

	top := container.NewVBox(form, actions, widget.NewSeparator())
	bottom := container.NewVBox(c.progress, c.status)
	c.content = container.NewBorder(top, bottom, nil, nil, c.table)
	return c
}

func (c *campaignView) updateCell(id widget.TableCellID, o fyne.CanvasObject) {
	l := o.(*widget.Label)
	if id.Row >= len(c.jobs) {
		return
	}
	j := c.jobs[id.Row]
	l.Importance = widget.MediumImportance
	switch id.Col {
	case 0:
		l.SetText(strconv.Itoa(j.Recipient.Row))
	case 1:
		l.SetText(j.Recipient.Email)
	case 2:
		l.SetText(j.Recipient.Name)
	case 3:
		if j.TemplatePath != "" {
			l.SetText(filepath.Base(j.TemplatePath))
		} else {
			l.SetText(j.Recipient.Template)
		}
	case 4:
		l.Importance = c.rows[id.Row].imp
		l.SetText(c.rows[id.Row].text)
	}
}

func (c *campaignView) collect(s *config.Settings) {
	s.From = strings.TrimSpace(c.from.Text)
	s.Subject = c.subject.Text
	s.RecipientsPath = strings.TrimSpace(c.recipientsPath.Text)
	s.TemplatePath = strings.TrimSpace(c.templatePath.Text)
	s.UseCSVTemplate = c.useCSVTemplate.Checked
	if d, err := c.delaySeconds(); err == nil {
		s.DelaySeconds = d
	}
}

func (c *campaignView) delaySeconds() (float64, error) {
	d, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(c.delay.Text), ",", "."), 64)
	if err != nil || d < 0 {
		return 0, errors.New("delay must be a number of seconds, e.g. 1 or 0.5")
	}
	return d, nil
}

func (c *campaignView) templateDir(s config.Settings) string {
	if s.TemplatePath != "" {
		return filepath.Dir(s.TemplatePath)
	}
	return filepath.Join(filepath.Dir(s.RecipientsPath), "templates")
}

// prepare reloads the CSV and templates from disk and renders every email.
func (c *campaignView) prepare() ([]campaign.Job, error) {
	c.a.collectSettings()
	s := c.a.settings
	if s.RecipientsPath == "" {
		return nil, errors.New("choose a recipients CSV file first")
	}
	list, err := recipients.Load(s.RecipientsPath, recipients.Options{Encoding: s.CSVEncoding, Delimiter: s.CSVDelimiter})
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", filepath.Base(s.RecipientsPath), err)
	}
	if len(list.Recipients) == 0 {
		return nil, fmt.Errorf("no recipients found in %s", filepath.Base(s.RecipientsPath))
	}
	c.headers = list.Headers
	return campaign.Prepare(list, campaign.Settings{
		Subject:         s.Subject,
		DefaultTemplate: s.TemplatePath,
		UseCSVTemplate:  s.UseCSVTemplate,
		TemplateDir:     c.templateDir(s),
	}), nil
}

func (c *campaignView) setJobs(jobs []campaign.Job) {
	c.jobs = jobs
	c.rows = make([]rowStatus, len(jobs))
	for i, j := range jobs {
		switch {
		case j.Err != nil:
			c.rows[i] = rowStatus{"Invalid: " + j.Err.Error(), widget.DangerImportance}
		case j.Warning != "":
			c.rows[i] = rowStatus{"Warning: " + j.Warning, widget.WarningImportance}
		default:
			c.rows[i] = rowStatus{"Ready", widget.MediumImportance}
		}
	}
	c.selected = -1
	c.table.UnselectAll()
	c.table.Refresh()
	c.progress.SetValue(0)
}

func countJobs(jobs []campaign.Job) (ready, warnings, invalid int) {
	for _, j := range jobs {
		switch {
		case j.Err != nil:
			invalid++
		case j.Warning != "":
			warnings++
			ready++
		default:
			ready++
		}
	}
	return
}

// check is the dry run: load everything and show what would be sent.
func (c *campaignView) check() {
	jobs, err := c.prepare()
	if err != nil {
		c.setJobs(nil)
		c.status.SetText(err.Error())
		return
	}
	c.setJobs(jobs)
	ready, warnings, invalid := countJobs(jobs)
	c.status.SetText(fmt.Sprintf("%d recipients: %d ready (%d with warnings), %d invalid and will be skipped. Nothing has been sent.",
		len(jobs), ready, warnings, invalid))
}

// pickJob returns the selected job, or the first valid one.
func (c *campaignView) pickJob() (int, error) {
	if c.selected >= 0 && c.selected < len(c.jobs) {
		if err := c.jobs[c.selected].Err; err != nil {
			return -1, fmt.Errorf("row %d is invalid: %w", c.jobs[c.selected].Recipient.Row, err)
		}
		return c.selected, nil
	}
	for i, j := range c.jobs {
		if j.Err == nil {
			return i, nil
		}
	}
	return -1, errors.New("there are no valid recipients")
}

func (c *campaignView) preview() {
	sel := c.selected
	jobs, err := c.prepare()
	if err != nil {
		dialog.ShowError(err, c.a.win)
		return
	}
	c.setJobs(jobs)
	if sel < len(jobs) {
		c.selected = sel
	}
	i, err := c.pickJob()
	if err != nil {
		dialog.ShowError(err, c.a.win)
		return
	}
	j := c.jobs[i]
	c.a.openInBrowser(j.Body)
	c.status.SetText(fmt.Sprintf("Preview opened in browser – To: %s, Subject: %s", j.Recipient.Email, j.Subject))
}

// readyToSend validates the sender, subject and delay and connects to Gmail.
func (c *campaignView) readyToSend() (svc *gmail.Service, from string, delay time.Duration, err error) {
	from, err = mailer.NormalizeAddress(c.from.Text)
	if err != nil {
		return nil, "", 0, fmt.Errorf("From: %w", err)
	}
	if strings.TrimSpace(c.subject.Text) == "" {
		return nil, "", 0, errors.New("enter a subject")
	}
	d, err := c.delaySeconds()
	if err != nil {
		return nil, "", 0, err
	}
	svc, err = c.a.gmailService()
	if err != nil {
		return nil, "", 0, err
	}
	return svc, c.from.Text, time.Duration(d * float64(time.Second)), nil
}

func (c *campaignView) sendTest() {
	sel := c.selected
	jobs, err := c.prepare()
	if err != nil {
		dialog.ShowError(err, c.a.win)
		return
	}
	c.setJobs(jobs)
	if sel < len(jobs) {
		c.selected = sel
	}
	i, err := c.pickJob()
	if err != nil {
		dialog.ShowError(err, c.a.win)
		return
	}
	svc, from, _, err := c.readyToSend()
	if err != nil {
		dialog.ShowError(err, c.a.win)
		return
	}
	j := c.jobs[i]
	msg := fmt.Sprintf("Send a test email to %s?\n\nIt is personalized for row %d (%s).", from, j.Recipient.Row, j.Recipient.Email)
	dialog.ShowConfirm("Send test email", msg, func(ok bool) {
		if !ok {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		c.cancel = cancel
		c.setBusy(true)
		c.status.SetText("Sending test email…")
		go func() {
			raw, err := mailer.Message{From: from, To: from, Subject: "[TEST] " + j.Subject, HTML: j.Body}.Bytes()
			if err == nil {
				err = mailer.GmailSender{Service: svc}.Send(ctx, raw)
			}
			cancel()
			fyne.Do(func() {
				c.cancel = nil
				c.setBusy(false)
				if err != nil {
					c.status.SetText("Test email failed.")
					dialog.ShowError(friendlyError(err), c.a.win)
					return
				}
				c.status.SetText("Test email sent to " + from + ".")
				dialog.ShowInformation("Test sent", "Test email sent to "+from+".\nCheck your inbox.", c.a.win)
			})
		}()
	}, c.a.win)
}

func (c *campaignView) sendAll() {
	jobs, err := c.prepare()
	if err != nil {
		dialog.ShowError(err, c.a.win)
		return
	}
	c.setJobs(jobs)
	ready, warnings, invalid := countJobs(jobs)
	if ready == 0 {
		dialog.ShowError(errors.New("there are no valid recipients"), c.a.win)
		return
	}
	svc, from, delay, err := c.readyToSend()
	if err != nil {
		dialog.ShowError(err, c.a.win)
		return
	}
	msg := fmt.Sprintf("Send %d emails from %s?\n\nSubject: %s", ready, from, c.subject.Text)
	if warnings > 0 {
		msg += fmt.Sprintf("\n\n%d rows have warnings (see the Status column).", warnings)
	}
	if invalid > 0 {
		msg += fmt.Sprintf("\n%d invalid rows will be skipped.", invalid)
	}
	dialog.ShowConfirm("Send emails", msg, func(ok bool) {
		if ok {
			c.run(svc, from, jobs, delay)
		}
	}, c.a.win)
}

func (c *campaignView) run(svc *gmail.Service, from string, jobs []campaign.Job, delay time.Duration) {
	log, err := campaign.CreateLog(filepath.Dir(c.a.settings.RecipientsPath))
	if err != nil {
		dialog.ShowError(fmt.Errorf("create send log: %w", err), c.a.win)
		return
	}
	c.logPath = log.Path
	c.logBtn.Enable()

	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.setBusy(true)

	var sent, failed, skipped int
	var authErr error
	total := float64(len(jobs))

	go func() {
		campaign.Run(ctx, mailer.GmailSender{Service: svc}, from, jobs, delay, func(i int, o campaign.Outcome, err error) {
			log.Write(jobs[i], o, err)
			if o == campaign.Failed && isAuthError(err) {
				cancel() // every following send would fail the same way
			}
			fyne.Do(func() {
				switch o {
				case campaign.Sent:
					sent++
					c.rows[i] = rowStatus{"Sent", widget.SuccessImportance}
				case campaign.Failed:
					failed++
					c.rows[i] = rowStatus{"Failed: " + err.Error(), widget.DangerImportance}
					if isAuthError(err) {
						authErr = err
					}
				case campaign.Skipped:
					skipped++
					c.rows[i] = rowStatus{"Skipped: " + err.Error(), widget.LowImportance}
				}
				c.progress.SetValue(float64(i+1) / total)
				c.status.SetText(fmt.Sprintf("Sending %d/%d – sent %d, failed %d, skipped %d", i+1, len(jobs), sent, failed, skipped))
				c.table.Refresh()
			})
		})
		stopped := ctx.Err() != nil
		cancel()
		log.Close()

		fyne.Do(func() {
			c.cancel = nil
			c.setBusy(false)
			summary := fmt.Sprintf("Sent %d, failed %d, skipped %d.\nLog: %s", sent, failed, skipped, log.Path)
			if stopped {
				summary = "Stopped before all emails were sent.\n" + summary
			}
			c.status.SetText(strings.ReplaceAll(summary, "\n", " "))
			if authErr != nil {
				dialog.ShowError(friendlyError(authErr), c.a.win)
				return
			}
			dialog.ShowInformation("Done", summary, c.a.win)
		})
	}()
}

func (c *campaignView) sending() bool { return c.cancel != nil }

func (c *campaignView) stop() {
	if c.cancel != nil {
		c.cancel()
		c.status.SetText("Stopping…")
	}
}

func (c *campaignView) setBusy(busy bool) {
	for _, b := range c.buttons {
		if busy {
			b.Disable()
		} else {
			b.Enable()
		}
	}
	if busy {
		c.stopBtn.Enable()
	} else {
		c.stopBtn.Disable()
	}
}

func isAuthError(err error) bool {
	s := err.Error()
	return strings.Contains(s, "invalid_grant") || strings.Contains(s, "Error 401") || strings.Contains(s, "not logged in")
}

func friendlyError(err error) error {
	if isAuthError(err) {
		return fmt.Errorf("%w\n\nYour Google login has expired or was revoked. Log in again on the \"Account & settings\" tab", err)
	}
	return err
}
