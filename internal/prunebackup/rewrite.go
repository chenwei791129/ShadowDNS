package prunebackup

import (
	"strings"

	"go.uber.org/zap"
)

// LineRange is a 1-based inclusive [Start, End] span of physical lines
// pruneFile will drop from the output.
type LineRange struct {
	Start int
	End   int
}

// pruneFile drops lines in deleteRanges, plus blank and stand-alone `;`
// comment lines. Retained lines are written back byte-identical so trailing
// `;` comments on RR lines survive. `$GENERATE` directives are never
// generated as deletion candidates and trigger an INFO log so the operator
// can review them manually.
func pruneFile(lines []string, deleteRanges []LineRange, logger *zap.Logger, filePath string) []string {
	if logger == nil {
		logger = zap.NewNop()
	}

	inDelete := make([]bool, len(lines)+1)
	for _, r := range deleteRanges {
		for i := r.Start; i <= r.End; i++ {
			if i >= 1 && i <= len(lines) {
				inDelete[i] = true
			}
		}
	}

	out := make([]string, 0, len(lines))
	for i, line := range lines {
		lineNo := i + 1
		if inDelete[lineNo] {
			continue
		}
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" || trimmed[0] == ';' {
			// Blank or stand-alone comment line — always dropped.
			continue
		}
		if trimmed[0] == '$' && isGenerateDirective(trimmed) {
			logger.Sugar().Infow("opaque directive retained",
				"file", filePath,
				"line", lineNo,
				"directive", directiveGenerate,
			)
		}
		out = append(out, line)
	}
	return out
}

// isGenerateDirective returns true if trimmed (already left-trimmed of
// whitespace) begins with `$GENERATE` followed by whitespace or EOL.
func isGenerateDirective(trimmed string) bool {
	if len(trimmed) < len(directiveGenerate) {
		return false
	}
	if !strings.EqualFold(trimmed[:len(directiveGenerate)], directiveGenerate) {
		return false
	}
	if len(trimmed) == len(directiveGenerate) {
		return true
	}
	next := trimmed[len(directiveGenerate)]
	return next == ' ' || next == '\t'
}
