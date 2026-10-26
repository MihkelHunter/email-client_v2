package campaign

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MihkelHunter/email-client_v2/internal/recipients"
)

func TestRender(t *testing.T) {
	out, missing := Render("<p>Tere, {{ Name }}! {{city}}</p>", map[string]string{"name": "Tom & <Jerry>"}, true)
	if out != "<p>Tere, Tom &amp; &lt;Jerry&gt;! </p>" {
		t.Errorf("out = %q", out)
	}
	if !reflect.DeepEqual(missing, []string{"city"}) {
		t.Errorf("missing = %v", missing)
	}
}

func TestPrepare(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "default.html"), []byte("D {{name}}"), 0o600)
	os.WriteFile(filepath.Join(dir, "other.html"), []byte("O {{name}}"), 0o600)
	list := &recipients.List{Recipients: []recipients.Recipient{
		{Row: 1, Email: "a@b.ee", Name: "A", Fields: map[string]string{"name": "A"}},
		{Row: 2, Email: "c@d.ee", Template: "other", Name: "C", Fields: map[string]string{"name": "C"}},
		{Row: 3, Email: "A@B.ee", Fields: map[string]string{}},
		{Row: 4, Email: "not-an-email", Fields: map[string]string{}},
		{Row: 5, Email: "e@f.ee", Template: "missing", Fields: map[string]string{}},
		{Row: 6, Email: "g@h.ee", Fields: map[string]string{}},
	}}
	jobs := Prepare(list, Settings{
		Subject:         "Hi {{name}}",
		DefaultTemplate: filepath.Join(dir, "default.html"),
		UseCSVTemplate:  true,
		TemplateDir:     dir,
	})
	if jobs[0].Body != "D A" || jobs[0].Subject != "Hi A" || jobs[0].Err != nil {
		t.Errorf("job 0: %+v", jobs[0])
	}
	if jobs[1].Body != "O C" {
		t.Errorf("job 1 body = %q", jobs[1].Body)
	}
	if jobs[2].Err == nil || !strings.Contains(jobs[2].Err.Error(), "duplicate") {
		t.Errorf("job 2 should be a duplicate: %v", jobs[2].Err)
	}
	if jobs[3].Err == nil {
		t.Error("job 3 should be invalid")
	}
	if jobs[4].Err == nil {
		t.Error("job 4 should fail on missing template")
	}
	if jobs[5].Err != nil || jobs[5].Warning == "" {
		t.Errorf("job 5 should warn about empty name: %+v", jobs[5])
	}
}
