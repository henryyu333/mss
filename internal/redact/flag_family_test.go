package redact

import (
	"strings"
	"testing"
)

// The flag family beyond `--password`. `gpg --passphrase`, `borg --passphrase`
// and the `--secret`, `--token` and `--api-key` every CLI with an account
// behind it takes all reached `mss show` and `mss share` in the clear (#3596).
func TestASecretHandedToAFlagIsMasked(t *testing.T) {
	for _, line := range []string{
		"gpg --passphrase GpgPassphrase2026 --decrypt f.gpg",
		"gpg --passphrase=GpgPassphrase2026 --decrypt f.gpg",
		"borg create --passphrase BorgPassphrase2026 repo::x",
		"app --secret MyAppSecret2026 run",
		"app --token MyAppToken2026 run",
		"app --api-key MyApiKeyValue2026 run",
		"app --api_key MyApiKeyValue2026 run",
		"app --access-token AccessTokenValue2026",
		"app --client-secret ClientSecretValue2026",
		"mysql --password=MyRootPass2026",
	} {
		got, counts := Text(line)
		if !strings.Contains(got, "[redacted") {
			t.Errorf("a secret handed to a flag survived: %q -> %q", line, got)
		}
		if counts["credential"] == 0 {
			t.Errorf("nothing counted for %q", line)
		}
	}
}

// And the other half: a sentence about a flag is not a flag with a value. The
// pattern took any non-space run after the flag, so "run with --password from
// the keychain" lost "from" and counted it as a secret found.
func TestASentenceAboutAFlagKeepsItsNextWord(t *testing.T) {
	for _, line := range []string{
		"run with --password from the keychain",
		"pass the --token to the CLI",
		"the --secret flag reads a file",
		"use --api-key when the account needs one",
		"app --password --verbose",
		"set --passphrase only for encrypted repos",
	} {
		got, counts := Text(line)
		if got != line {
			t.Errorf("prose about a flag was redacted: %q -> %q", line, got)
		}
		if counts.Total() != 0 {
			t.Errorf("prose about a flag was counted as a secret: %q", line)
		}
	}
}

// The guard is only for the space-separated form. `--password=x` is a shell
// assignment: whatever follows the equals sign is the value, word or not.
func TestAnAssignedFlagValueIsMaskedWhateverItLooksLike(t *testing.T) {
	for _, line := range []string{
		"app --password=secretword",
		"app --token=lowercase",
	} {
		if got, _ := Text(line); !strings.Contains(got, "[redacted") {
			t.Errorf("an assigned flag value survived: %q -> %q", line, got)
		}
	}
}

// The gate is a necessary condition, not a filter: a text the gate rejects must
// be one the pattern could not have matched anyway.
func TestTheFlagGateNeverHidesAMatch(t *testing.T) {
	for _, line := range []string{
		"app --password=MyRootPass2026",
		"app --token MyAppToken2026",
		"DATABASE_PASSWORD=DotenvPass2026",
		"gpg --passphrase GpgPassphrase2026",
		"--CLIENT_SECRET=UpperCaseSecret2026",
	} {
		lower := strings.ToLower(line)
		if m := passwordFlagRE.FindStringSubmatch(line); m != nil && !flagHintNearby(lower) {
			t.Errorf("the gate rejected a line the pattern matches: %q", line)
		}
	}
}
