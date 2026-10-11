// Package legacy is the pipe-delimited "0|" protocol tunneled inside
// binary command 4224 on the v10 socket, and spoken directly by v4 clients.
package legacy

import "strings"

// Split breaks a legacy message into pipe fields.
// A trailing '#' or NUL, used by the chat socket, is stripped.
func Split(message string) []string {
	message = strings.TrimRight(message, "#\n\r\x00")
	if message == "" {
		return nil
	}
	return strings.Split(message, "|")
}

// Join rebuilds a legacy message. The caller adds any chat terminator.
func Join(parts ...string) string {
	return strings.Join(parts, "|")
}

// Std is the system-message packet broadcast as 0|A|STD|<text>.
func Std(text string) string {
	return Join("0", "A", "STD", text)
}
