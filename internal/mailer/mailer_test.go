package mailer

import (
	"strings"
	"testing"
)

func TestNormalizeAddress(t *testing.T) {
	cases := map[string]string{
		"a@b.ee":               "<a@b.ee>",
		" Mari <mari@õun.ee> ": `"Mari" <mari@xn--un-bka.ee>`,
	}
	for in, want := range cases {
		got, err := NormalizeAddress(in)
		if err != nil || got != want {
			t.Errorf("NormalizeAddress(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "Email", "a@b.ee\r\nBcc: x@y.ee"} {
		if _, err := NormalizeAddress(bad); err == nil {
			t.Errorf("NormalizeAddress(%q) should fail", bad)
		}
	}
}

func TestMessageBytes(t *testing.T) {
	raw, err := Message{From: "me@b.ee", To: "you@b.ee", Subject: "Tere, Jüri!", HTML: "<p>Õun</p>"}.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{"From: <me@b.ee>\r\n", "To: <you@b.ee>\r\n", "Subject: =?utf-8?q?", "Content-Transfer-Encoding: quoted-printable", "<p>=C3=95un</p>"} {
		if !strings.Contains(s, want) {
			t.Errorf("message missing %q:\n%s", want, s)
		}
	}
}
