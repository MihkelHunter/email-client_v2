package recipients

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/text/encoding/charmap"
)

func TestParseWithHeader(t *testing.T) {
	list, err := parse("Email;Template;Greeting;City\na@b.ee;welcome;Mari;Tartu\n\n;;;\nc@d.ee;;Jüri;\n", ';')
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Recipients) != 2 {
		t.Fatalf("got %d recipients, want 2", len(list.Recipients))
	}
	r := list.Recipients[0]
	if r.Email != "a@b.ee" || r.Template != "welcome" || r.Name != "Mari" || r.Fields["city"] != "Tartu" || r.Row != 2 {
		t.Errorf("unexpected first recipient: %+v", r)
	}
	if list.Recipients[1].Name != "Jüri" || list.Recipients[1].Row != 5 {
		t.Errorf("unexpected second recipient: %+v", list.Recipients[1])
	}
}

func TestParseWithoutHeader(t *testing.T) {
	list, err := parse("a@b.ee,welcome,Mari\nc@d.ee\n", ',')
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Recipients) != 2 || list.Headers != nil {
		t.Fatalf("unexpected list: %+v", list)
	}
	if r := list.Recipients[1]; r.Email != "c@d.ee" || r.Template != "" || r.Name != "" {
		t.Errorf("unexpected second recipient: %+v", r)
	}
}

func TestHeaderWithoutEmailColumn(t *testing.T) {
	if _, err := parse("Foo;Bar\nx;y\n", ';'); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadAutoEncoding(t *testing.T) {
	dir := t.TempDir()
	cp1257, _ := charmap.Windows1257.NewEncoder().String("a@b.ee;x;Jüri Õun\n")
	for name, content := range map[string]string{
		"utf8.csv":   "\xef\xbb\xbfa@b.ee;x;Jüri Õun\n",
		"cp1257.csv": cp1257,
	} {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte(content), 0o600)
		list, err := Load(p, Options{Encoding: "auto", Delimiter: "auto"})
		if err != nil {
			t.Fatal(err)
		}
		if got := list.Recipients[0].Name; got != "Jüri Õun" {
			t.Errorf("%s: name = %q", name, got)
		}
		if got := list.Recipients[0].Email; got != "a@b.ee" {
			t.Errorf("%s: email = %q", name, got)
		}
	}
}
