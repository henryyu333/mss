package redact

import (
	"strings"
	"testing"
)

// A password handed to a program as a long flag, at the length people actually
// choose. The key-value patterns take any value of sixteen characters or more,
// which is right where a short value is as likely to be a word — and wrong
// after `--password`, where the flag says what the value is. Measured through
// an index pass before the fix: `mysql --password=MyRootPass2026` (fourteen)
// and `app --password=Pass2026Short` (thirteen) reached `mss show` and `mss
// share` in the clear (#3572).
func TestAPasswordGivenAsAFlagIsRedactedWhateverItsLength(t *testing.T) {
	for _, line := range []string{
		"mysql --password=MyRootPass2026 -u root",
		"mysql --password MyRootPass2026 -u root",
		"mysqldump --password=DumpPass2026 -u root db",
		"psql --password=Psql2026 -U app",
		"app --password=Short1",
		"app --passwd=Short1",
		"deploy --pwd QuietSecret9",
		"someprog --password=x7Kq",
	} {
		got, counts := Text(line)
		if strings.Contains(got, "Pass2026") || strings.Contains(got, "Short1") ||
			strings.Contains(got, "Psql2026") || strings.Contains(got, "x7Kq") ||
			strings.Contains(got, "QuietSecret9") {
			t.Errorf("the value survived: %q -> %q", line, got)
		}
		if counts["credential"] == 0 {
			t.Errorf("nothing was counted for %q -> %q", line, got)
		}
	}
}

// What follows the flag is not always a secret, and redacting those costs the
// reader the sentence while hiding nothing.
func TestAPlaceholderAfterThePasswordFlagIsLeftAlone(t *testing.T) {
	for _, line := range []string{
		"mysql --password=$DB_PASSWORD -u root",
		"mysql --password=${DB_PASSWORD} -u root",
		"app --password=<your-password>",
		"app --password=changeme",
		"app --password=none",
		"app --password=null",
		"docs: pass --password %s at the end",
	} {
		if got, _ := Text(line); got != line {
			t.Errorf("a placeholder was redacted: %q -> %q", line, got)
		}
	}
}

// The bare key keeps the floor it had: `password: hunter2` is a near miss by
// an older decision this does not touch, and prose that merely says the word
// stays readable.
func TestTheBareWordKeepsItsOldReading(t *testing.T) {
	for _, line := range []string{
		"password: hunter2",
		"the password prompt appeared twice",
		"password authentication failed for user deploy",
		"the password was rotated on tuesday",
	} {
		if got, _ := Text(line); got != line {
			t.Errorf("prose was redacted: %q -> %q", line, got)
		}
	}
}
