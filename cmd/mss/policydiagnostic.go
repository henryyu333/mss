package main

import (
	"fmt"
	"io"

	"github.com/henryyu333/mss/internal/policy"
)

// warnPolicyDiagnostic is called once by run for a recall invocation, not by
// each filter or policy load. Diagnostics stay on stderr, leaving JSON and
// stdout unchanged. Do not echo config keys, values or parse errors here:
// doctor is the explicit surface for inspecting the user's configuration.
func warnPolicyDiagnostic(w io.Writer) {
	exists, unknown, err := policy.Diagnose()
	if !exists {
		return
	}
	if err != nil {
		fmt.Fprintln(w, "mss: warning: recall policy could not be read or parsed; using the permissive default (all origins allowed); run `mss doctor` to diagnose and fix it")
		return
	}
	if len(unknown) > 0 {
		fmt.Fprintln(w, "mss: warning: recall policy contains keys mss does not consult; those rules have no effect; run `mss doctor` to diagnose and fix it")
	}
}
