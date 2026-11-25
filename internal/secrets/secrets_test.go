package secrets

import (
	"strings"
	"testing"
)

func TestScanDetects(t *testing.T) {
	cases := map[string]string{
		"aws":     "key AKIAIOSFODNN7EXAMPLE here",
		"github":  "token ghp_" + strings.Repeat("a1B2", 9),
		"jwt":     "Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dBjftJeZ4CVPmB92K27uhb",
		"url":     "postgres://admin:S3cretPass@db.internal:5432/app",
		"assign":  `password = "hunter2hunter2"`,
		"private": "-----BEGIN RSA PRIVATE KEY-----\nMIIBOgIBAAJB\n-----END RSA PRIVATE KEY-----",
		"anthr":   "sk-ant-api03-abcdefghijklmnopqrstuvwx",
		"stripe":  "sk_live_" + strings.Repeat("x9", 10),
	}
	for name, in := range cases {
		if len(Scan(in)) == 0 {
			t.Errorf("%s: expected a finding in %q", name, in)
		}
	}
}

func TestScanIgnoresPlaceholdersAndProse(t *testing.T) {
	safe := []string{
		"password: <your-password>",
		"API_KEY=${API_KEY}",
		"token = process.env.TOKEN_VALUE",
		"We rotated the token after the incident and documented the process.",
		"postgres://user@host/db",
		"secret: xxxxxxxx",
		"El password se guarda en el gestor de contraseñas",
	}
	for _, in := range safe {
		if f := Scan(in); len(f) != 0 {
			t.Errorf("false positive on %q: %+v", in, f)
		}
	}
}

func TestApplyRedactKeepsContextAndHidesValue(t *testing.T) {
	in := "Conexión: postgres://admin:S3cretPass@db:5432/app y listo"
	out, f, err := Apply(in, Redact)
	if err != nil || len(f) != 1 {
		t.Fatalf("err=%v findings=%d", err, len(f))
	}
	if strings.Contains(out, "S3cretPass") {
		t.Fatalf("secret leaked: %s", out)
	}
	if !strings.Contains(out, "postgres://admin:[REDACTED:url-credentials]@db:5432/app") {
		t.Fatalf("unexpected redaction: %s", out)
	}
}

func TestApplyBlockAndOff(t *testing.T) {
	in := "password = abcdefgh1234"
	if _, _, err := Apply(in, Block); err == nil {
		t.Fatal("expected block error")
	}
	out, f, err := Apply(in, Off)
	if err != nil || len(f) != 0 || out != in {
		t.Fatal("off must be a no-op")
	}
}
