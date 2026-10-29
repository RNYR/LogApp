// Package statusnames defines status names shared by all interface languages.
package statusnames

import "strings"

func Fixed(value string) string {
	switch strings.ToLower(value) {
	case "feasibility":
		return "Feasibility"
	case "devel":
		return "Devel"
	case "sales":
		return "Sales"
	default:
		return ""
	}
}
