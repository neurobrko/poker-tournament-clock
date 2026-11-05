package ui

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

func messageIsError(title string) bool {
	title = strings.ToLower(title)
	for _, marker := range []string{"failed", "unavailable", "locked", "invalid", "warning", "error"} {
		if strings.Contains(title, marker) {
			return true
		}
	}
	return false
}

func formatUserError(message string) string {
	message = strings.TrimSpace(message)
	if prefix, detail, ok := strings.Cut(message, ": "); ok &&
		strings.HasPrefix(strings.ToLower(prefix), "apply ") && strings.Contains(strings.ToLower(prefix), " event") {
		message = strings.TrimSpace(detail)
	}
	message = strings.TrimRight(message, " \t\r\n.:;!")
	if message == "" {
		return "Error:"
	}
	first, size := utf8.DecodeRuneInString(message)
	message = string(unicode.ToUpper(first)) + message[size:]
	return message + ":"
}
