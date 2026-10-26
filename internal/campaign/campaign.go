// Package campaign turns recipients and templates into personalized emails
// and sends them.
package campaign

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/MihkelHunter/email-client_v2/internal/mailer"
	"github.com/MihkelHunter/email-client_v2/internal/recipients"
)

var placeholderRE = regexp.MustCompile(`\{\{\s*([^{}\s]+)\s*\}\}`)

// Render replaces {{key}} placeholders (case-insensitive) with values from
// fields, HTML-escaping the values when escape is set. It returns the keys
// that had no value; those placeholders are replaced with an empty string.
func Render(tmpl string, fields map[string]string, escape bool) (string, []string) {
	var missing []string
	out := placeholderRE.ReplaceAllStringFunc(tmpl, func(m string) string {
		key := strings.ToLower(placeholderRE.FindStringSubmatch(m)[1])
		v := fields[key]
		if v == "" {
			missing = append(missing, key)
		}
		if escape {
			v = html.EscapeString(v)
		}
		return v
	})
	return out, dedupe(missing)
}

// Placeholders lists the distinct placeholder keys used in tmpl.
func Placeholders(tmpl string) []string {
	var keys []string
	for _, m := range placeholderRE.FindAllStringSubmatch(tmpl, -1) {
		keys = append(keys, strings.ToLower(m[1]))
	}
	return dedupe(keys)
}

type Settings struct {
	Subject string
	// DefaultTemplate is used for every recipient unless UseCSVTemplate is set
	// and the recipient's row names a template.
	DefaultTemplate string
	UseCSVTemplate  bool
	// TemplateDir is where template names from the CSV are looked up.
	TemplateDir string
}

type Job struct {
	Recipient    recipients.Recipient
	TemplatePath string
	Subject      string
	Body         string
	// Err means the row is invalid and will be skipped.
	Err error
	// Warning is a non-fatal problem, e.g. an empty {{name}}.
	Warning string
}

// Prepare validates every recipient and renders its subject and body.
func Prepare(list *recipients.List, s Settings) []Job {
	templates := map[string]templateFile{}
	seen := map[string]int{}
	jobs := make([]Job, 0, len(list.Recipients))

	for _, r := range list.Recipients {
		j := Job{Recipient: r}
		jobs = append(jobs, j)
		jp := &jobs[len(jobs)-1]

		addr, err := mailer.NormalizeAddress(r.Email)
		if err != nil {
			jp.Err = err
			continue
		}
		key := strings.ToLower(addr)
		if prev, dup := seen[key]; dup {
			jp.Err = fmt.Errorf("duplicate of row %d", prev)
			continue
		}
		seen[key] = r.Row

		jp.TemplatePath = s.DefaultTemplate
		if s.UseCSVTemplate && r.Template != "" {
			jp.TemplatePath = ResolveTemplate(s.TemplateDir, r.Template)
		}
		if jp.TemplatePath == "" {
			jp.Err = errors.New("no template selected")
			continue
		}
		tf, ok := templates[jp.TemplatePath]
		if !ok {
			b, err := os.ReadFile(jp.TemplatePath)
			tf = templateFile{content: string(b), err: err}
			templates[jp.TemplatePath] = tf
		}
		if tf.err != nil {
			jp.Err = fmt.Errorf("template %s: %w", filepath.Base(jp.TemplatePath), tf.err)
			continue
		}

		var missingBody, missingSubject []string
		jp.Body, missingBody = Render(tf.content, r.Fields, true)
		jp.Subject, missingSubject = Render(s.Subject, r.Fields, false)
		if missing := dedupe(append(missingSubject, missingBody...)); len(missing) > 0 {
			jp.Warning = "empty placeholder: " + strings.Join(missing, ", ")
		}
	}
	return jobs
}

type templateFile struct {
	content string
	err     error
}

// ResolveTemplate turns a template name from the CSV ("welcome" or
// "welcome.html") into a path inside dir.
func ResolveTemplate(dir, name string) string {
	if filepath.Ext(name) == "" {
		name += ".html"
	}
	if filepath.IsAbs(name) {
		return name
	}
	return filepath.Join(dir, name)
}

type Outcome int

const (
	Sent Outcome = iota
	Failed
	Skipped
)

func (o Outcome) String() string {
	switch o {
	case Sent:
		return "sent"
	case Failed:
		return "failed"
	}
	return "skipped"
}

// Run sends every valid job in order, waiting delay between sends. progress
// is called after each job. It stops early when ctx is cancelled.
func Run(ctx context.Context, sender mailer.Sender, from string, jobs []Job, delay time.Duration, progress func(i int, o Outcome, err error)) {
	for i, j := range jobs {
		if ctx.Err() != nil {
			return
		}
		if j.Err != nil {
			progress(i, Skipped, j.Err)
			continue
		}
		raw, err := mailer.Message{From: from, To: j.Recipient.Email, Subject: j.Subject, HTML: j.Body}.Bytes()
		if err == nil {
			err = sender.Send(ctx, raw)
		}
		if err != nil {
			progress(i, Failed, err)
		} else {
			progress(i, Sent, nil)
		}
		if delay > 0 && i < len(jobs)-1 {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return
			}
		}
	}
}

// Log records send results in a semicolon-separated CSV (opens in Excel).
type Log struct {
	Path string
	f    *os.File
	w    *csv.Writer
}

func CreateLog(dir string) (*Log, error) {
	path := filepath.Join(dir, "send-log-"+time.Now().Format("20060102-150405")+".csv")
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	f.WriteString("\xef\xbb\xbf") // BOM so Excel detects UTF-8
	w := csv.NewWriter(f)
	w.Comma = ';'
	w.Write([]string{"time", "row", "email", "name", "template", "status", "error"})
	w.Flush()
	return &Log{Path: path, f: f, w: w}, w.Error()
}

func (l *Log) Write(j Job, o Outcome, err error) error {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	l.w.Write([]string{
		time.Now().Format(time.DateTime),
		fmt.Sprint(j.Recipient.Row),
		j.Recipient.Email,
		j.Recipient.Name,
		filepath.Base(j.TemplatePath),
		o.String(),
		msg,
	})
	l.w.Flush()
	return l.w.Error()
}

func (l *Log) Close() error {
	l.w.Flush()
	return l.f.Close()
}

func dedupe(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	sort.Strings(s)
	out := s[:1]
	for _, v := range s[1:] {
		if v != out[len(out)-1] {
			out = append(out, v)
		}
	}
	return out
}
