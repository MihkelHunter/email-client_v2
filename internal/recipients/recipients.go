// Package recipients reads the recipients CSV file.
package recipients

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

// Encodings and Delimiters list the values accepted in Options.
var (
	Encodings  = []string{"auto", "utf-8", "windows-1257", "windows-1252"}
	Delimiters = []string{"auto", ";", ",", "tab"}
)

type Options struct {
	// Encoding is one of Encodings. "auto" uses UTF-8 when the file is valid
	// UTF-8 and Windows-1257 (Excel's default for Estonian) otherwise.
	Encoding string
	// Delimiter is one of Delimiters. "auto" picks the most common one in the
	// first line.
	Delimiter string
}

type Recipient struct {
	Row      int // line number in the file
	Email    string
	Template string
	Name     string
	// Fields holds every column by lower-case header name (plus "email",
	// "template" and "name"), used to fill {{placeholders}}.
	Fields map[string]string
}

type List struct {
	Recipients []Recipient
	// Headers are the column names from the header row, if there was one.
	Headers []string
}

// Load parses a recipients file. Without a header row the columns are
// email;template;name. With a header row the columns are found by name
// (Email/E-mail/Epost, Template/Mall, Name/Greeting/Nimi) and every column
// becomes a {{placeholder}}. A header row is detected when the first row
// contains no "@".
func Load(path string, opts Options) (*List, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text, err := decode(raw, opts.Encoding)
	if err != nil {
		return nil, err
	}
	return parse(text, delimiter(text, opts.Delimiter))
}

func parse(text string, comma rune) (*List, error) {
	r := csv.NewReader(strings.NewReader(text))
	r.Comma = comma
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.TrimLeadingSpace = true

	list := &List{}
	emailCol, tmplCol, nameCol := 0, 1, 2
	first := true
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		line, _ := r.FieldPos(0)
		if blank(rec) {
			continue
		}
		if first {
			first = false
			if !strings.Contains(strings.Join(rec, ""), "@") {
				list.Headers = trimAll(rec)
				emailCol, tmplCol, nameCol = columns(list.Headers)
				if emailCol < 0 {
					return nil, fmt.Errorf("header row has no email column (expected one named \"Email\"): %v", rec)
				}
				continue
			}
		}

		rec = trimAll(rec)
		rcp := Recipient{
			Row:      line,
			Email:    cell(rec, emailCol),
			Template: cell(rec, tmplCol),
			Name:     cell(rec, nameCol),
			Fields:   map[string]string{},
		}
		for i, h := range list.Headers {
			if h != "" {
				rcp.Fields[strings.ToLower(h)] = cell(rec, i)
			}
		}
		rcp.Fields["email"] = rcp.Email
		rcp.Fields["template"] = rcp.Template
		rcp.Fields["name"] = rcp.Name
		list.Recipients = append(list.Recipients, rcp)
	}
	return list, nil
}

func columns(headers []string) (email, tmpl, name int) {
	email, tmpl, name = -1, -1, -1
	for i, h := range headers {
		switch strings.ToLower(h) {
		case "email", "e-mail", "epost", "e-post", "mail", "address":
			email = i
		case "template", "mall":
			tmpl = i
		case "name", "greeting", "nimi":
			name = i
		}
	}
	return email, tmpl, name
}

func decode(raw []byte, encoding string) (string, error) {
	if encoding == "" || encoding == "auto" {
		encoding = "windows-1257"
		if utf8.Valid(raw) {
			encoding = "utf-8"
		}
	}
	switch encoding {
	case "utf-8":
		return string(bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))), nil
	case "windows-1257":
		b, err := charmap.Windows1257.NewDecoder().Bytes(raw)
		return string(b), err
	case "windows-1252":
		b, err := charmap.Windows1252.NewDecoder().Bytes(raw)
		return string(b), err
	}
	return "", fmt.Errorf("unknown encoding %q", encoding)
}

func delimiter(text, opt string) rune {
	switch opt {
	case ";":
		return ';'
	case ",":
		return ','
	case "tab":
		return '\t'
	}
	firstLine, _, _ := strings.Cut(text, "\n")
	best, bestCount := ';', 0
	for _, d := range []rune{';', ',', '\t'} {
		if n := strings.Count(firstLine, string(d)); n > bestCount {
			best, bestCount = d, n
		}
	}
	return best
}

func cell(rec []string, i int) string {
	if i < 0 || i >= len(rec) {
		return ""
	}
	return rec[i]
}

func trimAll(rec []string) []string {
	out := make([]string, len(rec))
	for i, s := range rec {
		out[i] = strings.TrimSpace(s)
	}
	return out
}

func blank(rec []string) bool {
	for _, s := range rec {
		if strings.TrimSpace(s) != "" {
			return false
		}
	}
	return true
}
