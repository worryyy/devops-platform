package deploy

import (
	"regexp"
	"strings"
)

var sensitiveLinePattern = regexp.MustCompile(`(?i)(password|token|secret|credential|kubeconfig|private_key|api_key)(\s*[:=]\s*).+`)

func sanitizeText(value string) string {
	lines := strings.Split(value, "\n")
	for i, line := range lines {
		if sensitiveLinePattern.MatchString(line) {
			lines[i] = sensitiveLinePattern.ReplaceAllString(line, "$1$2******")
		}
	}
	return strings.Join(lines, "\n")
}
