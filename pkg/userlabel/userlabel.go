// Package userlabel is how the runtime names a person in text a human reads.
// It follows the same cascade as Miren Cloud, so a user reads the same in the
// CLI as in cloud's notifications.
package userlabel

import "strings"

// Label names a person in one line: "Ada Lovelace (ada@example.com)" when
// both name and email are known, otherwise whichever of email, name, or
// subject is set, in that order. The email beats the name on its own
// because it is unambiguous; the subject is the last resort because it is
// usually an opaque id. An empty result means there was nothing to go on,
// and the caller picks the placeholder that fits.
func Label(name, email, subject string) string {
	name, email, subject = strings.TrimSpace(name), strings.TrimSpace(email), strings.TrimSpace(subject)
	switch {
	case name != "" && email != "":
		return name + " (" + email + ")"
	case email != "":
		return email
	case name != "":
		return name
	default:
		return subject
	}
}
