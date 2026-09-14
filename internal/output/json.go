package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// WriteJSON renders the full report as indented JSON.
// This output is designed to be consumed by Python, Grafana, or Excel
// for post-benchmark analysis (Section 20).
func WriteJSON(w io.Writer, report *FullReport) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

// WriteJSONFile writes the report to a file at the given path.
func WriteJSONFile(path string, report *FullReport) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating output file %s: %w", path, err)
	}
	defer f.Close()

	return WriteJSON(f, report)
}

// WriteReport dispatches to the appropriate formatter(s) based on
// the output format configuration.
//
//	"text" → stdout only
//	"json" → JSON to stdout (or file if configured)
//	"both" → text to stdout, JSON to file
func WriteReport(report *FullReport, format, filePath string) error {
	switch format {
	case "json":
		if filePath != "" {
			return WriteJSONFile(filePath, report)
		}
		return WriteJSON(os.Stdout, report)

	case "both":
		// Text to stdout.
		if err := WriteText(os.Stdout, report); err != nil {
			return fmt.Errorf("writing text output: %w", err)
		}
		// JSON to file (required for "both").
		if filePath != "" {
			if err := WriteJSONFile(filePath, report); err != nil {
				return fmt.Errorf("writing JSON file: %w", err)
			}
			fmt.Fprintf(os.Stderr, "\nJSON report written to: %s\n", filePath)
		} else {
			fmt.Fprintln(os.Stderr, "\nWarning: output.format=both but no output.file specified; JSON output skipped.")
		}
		return nil

	default: // "text"
		return WriteText(os.Stdout, report)
	}
}
