package cli

import (
	"fmt"
	"io"
	"strings"

	"opperator/internal/doctor"
)

func Doctor(out io.Writer) (int, error) {
	report := doctor.GenerateReport()

	fmt.Fprintln(out, "Opperator Doctor Report")
	fmt.Fprintln(out, strings.Repeat("-", 26))

	for _, check := range report.Checks {
		fmt.Fprintf(out, "%s %s - %s\n", formatStatus(check.Status), check.Name, check.Summary)
		for _, detail := range check.Details {
			fmt.Fprintf(out, "    %s\n", detail)
		}
		for _, action := range check.Actions {
			fmt.Fprintf(out, "    -> %s\n", action)
		}
		fmt.Fprintln(out)
	}

	exitCode := report.ExitCode()
	if exitCode == 0 {
		fmt.Fprintln(out, "All checks completed")
	} else {
		fmt.Fprintln(out, "One or more checks failed")
	}

	return exitCode, nil
}

func formatStatus(status doctor.Status) string {
	switch status {
	case doctor.StatusOK:
		return "[OK ]"
	case doctor.StatusWarn:
		return "[WARN]"
	case doctor.StatusFail:
		return "[FAIL]"
	default:
		return "[    ]"
	}
}
