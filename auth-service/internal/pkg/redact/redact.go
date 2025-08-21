package redact

import "strings"

func Email(s string) string {
	parts := strings.Split(s, "@")
	if len(parts) != 2 {
		return "***"
	}

	local, domain := parts[0], parts[1]
	if len(local) > 2 {
		local = local[:2] + "***"
	} else {
		local = "***"
	}

	return local + "@" + domain
}

func Token() string    { return "[REDACTED_TOKEN]" }
func Password() string { return "[REDACTED_PASSWORD]" }
