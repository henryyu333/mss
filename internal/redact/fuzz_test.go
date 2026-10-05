package redact

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// The redactor is the security boundary and it takes arbitrary transcript text,
// yet it had no fuzz target while the parsers that feed it have eight. Three
// invariants, all of which something downstream already assumes:
//
//   - text in, text out. A record that came back invalid UTF-8 would reach
//     `mss sync export` and a peer's index, which is what #1740 pins for the
//     parsers.
//   - redacting twice changes nothing more. `mss share` and `mss sync export`
//     redact again over text the index already masked, so a pattern that eats
//     its own marker would mangle an answer on the second pass — and the
//     redaction floor's whole premise is that re-reading a store converges.
//   - the count and the marker agree. A surface that prints "0 secrets masked"
//     beside a masked line, or the reverse, is worse than either alone.
func FuzzText(f *testing.F) {
	for _, seed := range []string{
		"", "plain text with no secrets",
		"password: hunter2",
		"app --password MyRealPass2026",
		"run with --password from the keychain",
		"gpg --passphrase GpgPassphrase2026 --decrypt f.gpg",
		"export GITHUB_TOKEN=ghp_A1b2C3d4E5f6G7h8I9j0K1l2M3n4O5p6Q7r8",
		"postgres://app:S3cretPass@db.internal:5432/prod",
		"пароль: БазаПароль2026годаДлинный",
		"密码: 非常に長いパスワード2026",
		"Discussions включены](https://example.com/a)",
		"-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA7x\n-----END RSA PRIVATE KEY-----",
		"Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.abcdefghijk",
		"[redacted:credential] and --password [redacted:credential]",
		"\x00\xff\xfe not utf-8 at all",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, in string) {
		out, counts := Text(in)
		if utf8.ValidString(in) && !utf8.ValidString(out) {
			t.Fatalf("valid utf-8 in, invalid out: %q -> %q", in, out)
		}
		again, _ := Text(out)
		if again != out {
			t.Fatalf("redacting twice changed the text:\n once: %q\ntwice: %q", out, again)
		}
		marked := strings.Contains(out, "[redacted:")
		if marked != (counts.Total() > 0) && !strings.Contains(in, "[redacted:") {
			t.Fatalf("marker=%v but count=%d for %q -> %q", marked, counts.Total(), in, out)
		}
	})
}
