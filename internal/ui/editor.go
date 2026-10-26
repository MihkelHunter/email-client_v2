package ui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/MihkelHunter/email-client_v2/internal/campaign"
)

const newTemplate = `<!DOCTYPE html>
<html>
  <head>
    <meta charset="utf-8">
  </head>
  <body style="font-family: Arial, sans-serif; font-size: 15px; color: #222222;">
    <p>Tere, {{name}}!</p>
    <p></p>
  </body>
</html>
`

// snippet is inserted around the selected text, or around placeholder text
// when nothing is selected.
type snippet struct {
	label, before, after, inner string
}

var snippets = []snippet{
	{"H2", "<h2>", "</h2>", "Heading"},
	{"P", "<p>", "</p>", "Text"},
	{"Bold", "<strong>", "</strong>", "bold text"},
	{"Italic", "<em>", "</em>", "italic text"},
	{"Link", `<a href="https://">`, "</a>", "link text"},
	{"Button", `<a href="https://" style="display:inline-block;padding:12px 24px;background:#1a73e8;color:#ffffff;text-decoration:none;border-radius:4px;">`, "</a>", "Button text"},
	{"Image", `<img src="https://" alt="`, `" width="600" style="max-width:100%;height:auto;">`, "description"},
	{"Line break", "", "<br>\n", ""},
}

// codeEntry is a monospace multi-line entry that saves on Ctrl+S.
type codeEntry struct {
	widget.Entry
	onSave func()
}

func newCodeEntry() *codeEntry {
	e := &codeEntry{}
	e.MultiLine = true
	e.Wrapping = fyne.TextWrapOff
	e.TextStyle = fyne.TextStyle{Monospace: true}
	e.ExtendBaseWidget(e)
	return e
}

func (e *codeEntry) TypedShortcut(s fyne.Shortcut) {
	if cs, ok := s.(*desktop.CustomShortcut); ok && cs.KeyName == fyne.KeyS && cs.Modifier == fyne.KeyModifierShortcutDefault {
		if e.onSave != nil {
			e.onSave()
		}
		return
	}
	e.Entry.TypedShortcut(s)
}

type editorView struct {
	a       *App
	content fyne.CanvasObject

	entry       *codeEntry
	title       *widget.Label
	info        *widget.Label
	placeholder *widget.Select

	path    string
	dirty   bool
	loading bool
}

func newEditorView(a *App) *editorView {
	e := &editorView{a: a}

	e.entry = newCodeEntry()
	e.entry.onSave = e.save
	e.entry.OnChanged = func(string) {
		if !e.loading {
			e.dirty = true
		}
		e.updateTitle()
	}
	e.title = widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	e.title.Truncation = fyne.TextTruncateEllipsis
	e.info = widget.NewLabel("")

	fileBar := container.NewHBox(
		widget.NewButtonWithIcon("New", theme.DocumentCreateIcon(), func() {
			e.confirmDiscard(func() { e.load("", newTemplate) })
		}),
		widget.NewButtonWithIcon("Open", theme.FolderOpenIcon(), func() {
			e.confirmDiscard(func() {
				a.pickFile(e.startDir(), []string{".html", ".htm"}, func(p string) { e.open(p, nil) })
			})
		}),
		widget.NewButtonWithIcon("Save", theme.DocumentSaveIcon(), e.save),
		widget.NewButtonWithIcon("Save as", theme.DocumentSaveIcon(), func() { e.saveAs(nil) }),
		widget.NewSeparator(),
		widget.NewButtonWithIcon("Preview in browser", theme.VisibilityIcon(), e.preview),
		widget.NewButtonWithIcon("Use in campaign", theme.ConfirmIcon(), e.useInCampaign),
	)

	snippetBar := container.NewHBox()
	for _, s := range snippets {
		snippetBar.Add(widget.NewButton(s.label, func() { e.insert(s) }))
	}
	e.placeholder = widget.NewSelect(nil, func(key string) {
		if key == "" {
			return
		}
		e.insert(snippet{before: "{{" + key + "}}"})
		e.placeholder.ClearSelected()
	})
	e.placeholder.PlaceHolder = "Insert {{placeholder}}"
	e.refreshPlaceholders()
	snippetBar.Add(e.placeholder)

	top := container.NewVBox(e.title, fileBar, snippetBar)
	e.content = container.NewBorder(top, e.info, nil, nil, e.entry)

	if p := a.settings.TemplatePath; p != "" {
		if b, err := os.ReadFile(p); err == nil {
			e.load(p, string(b))
			return e
		}
	}
	e.load("", newTemplate)
	return e
}

func (e *editorView) load(path, text string) {
	e.loading = true
	e.entry.SetText(text)
	e.loading = false
	e.path = path
	e.dirty = false
	e.updateTitle()
}

// open loads a template file, asking first if there are unsaved changes.
func (e *editorView) open(path string, then func()) {
	e.confirmDiscard(func() {
		b, err := os.ReadFile(path)
		if err != nil {
			dialog.ShowError(err, e.a.win)
			return
		}
		e.load(path, string(b))
		if then != nil {
			then()
		}
	})
}

func (e *editorView) confirmDiscard(next func()) {
	if !e.dirty {
		next()
		return
	}
	dialog.ShowConfirm("Unsaved changes", "Discard unsaved changes to the template?", func(ok bool) {
		if ok {
			next()
		}
	}, e.a.win)
}

func (e *editorView) save() {
	if e.path == "" {
		e.saveAs(nil)
		return
	}
	if err := os.WriteFile(e.path, []byte(e.entry.Text), 0o644); err != nil {
		dialog.ShowError(err, e.a.win)
		return
	}
	e.dirty = false
	e.updateTitle()
}

func (e *editorView) saveAs(then func()) {
	d := dialog.NewFileSave(func(w fyne.URIWriteCloser, err error) {
		if err != nil {
			dialog.ShowError(err, e.a.win)
			return
		}
		if w == nil {
			return
		}
		w.Close()
		p := filepath.FromSlash(w.URI().Path())
		if filepath.Ext(p) == "" {
			os.Remove(p)
			p += ".html"
		}
		e.path = p
		e.save()
		if then != nil && !e.dirty {
			then()
		}
	}, e.a.win)
	d.SetFileName("template.html")
	setDialogLocation(d, e.startDir())
	d.Resize(fyne.NewSize(900, 600))
	d.Show()
}

func (e *editorView) startDir() string {
	if e.path != "" {
		return e.path
	}
	if p := e.a.campaign.templatePath.Text; p != "" {
		return p
	}
	return e.a.campaign.recipientsPath.Text
}

// insert wraps the selection (or the snippet's sample text) in the snippet.
// It goes through a paste so it replaces the selection and supports undo.
func (e *editorView) insert(s snippet) {
	inner := e.entry.SelectedText()
	if inner == "" {
		inner = s.inner
	}
	cb := e.a.fyne.Clipboard()
	old := cb.Content()
	cb.SetContent(s.before + inner + s.after)
	e.entry.TypedShortcut(&fyne.ShortcutPaste{Clipboard: cb})
	cb.SetContent(old)
	e.a.win.Canvas().Focus(e.entry)
}

func (e *editorView) refreshPlaceholders() {
	keys := map[string]bool{"name": true, "email": true}
	for _, h := range e.a.campaign.headers {
		if h != "" {
			keys[strings.ToLower(h)] = true
		}
	}
	opts := make([]string, 0, len(keys))
	for k := range keys {
		opts = append(opts, k)
	}
	sort.Strings(opts)
	e.placeholder.SetOptions(opts)
}

// sampleFields personalizes the preview with the first valid recipient.
func (e *editorView) sampleFields() map[string]string {
	for _, j := range e.a.campaign.jobs {
		if j.Err == nil {
			return j.Recipient.Fields
		}
	}
	return map[string]string{"name": "Mari Maasikas", "email": "mari@example.com"}
}

func (e *editorView) preview() {
	body, _ := campaign.Render(e.entry.Text, e.sampleFields(), true)
	e.a.openInBrowser(body)
}

func (e *editorView) useInCampaign() {
	use := func() {
		e.a.campaign.templatePath.SetText(e.path)
		e.a.tabs.Select(e.a.campaignTab)
		e.a.campaign.check()
	}
	if e.path == "" || e.dirty {
		dialog.ShowConfirm("Save template", "The template must be saved before it can be used. Save now?", func(ok bool) {
			if !ok {
				return
			}
			if e.path == "" {
				e.saveAs(use)
				return
			}
			e.save()
			if !e.dirty {
				use()
			}
		}, e.a.win)
		return
	}
	use()
}

func (e *editorView) updateTitle() {
	name := "Untitled"
	if e.path != "" {
		name = e.path
	}
	if e.dirty {
		name += "  •  unsaved"
	}
	e.title.SetText(name)
	if keys := campaign.Placeholders(e.entry.Text); len(keys) > 0 {
		e.info.SetText("Placeholders used: {{" + strings.Join(keys, "}}, {{") + "}}   ·   Ctrl+S saves")
	} else {
		e.info.SetText("No placeholders used   ·   Ctrl+S saves")
	}
}
