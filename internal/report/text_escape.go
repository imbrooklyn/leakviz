package report

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// escapeTextScalar keeps profile-derived values on one visible report line.
// It preserves printable UTF-8 while making control characters, invalid bytes,
// and literal escape syntax unambiguous.
func escapeTextScalar(value string) string {
	var output strings.Builder
	for len(value) > 0 {
		r, size := utf8.DecodeRuneInString(value)
		if r == utf8.RuneError && size == 1 {
			fmt.Fprintf(&output, `\x%02x`, value[0])
			value = value[1:]
			continue
		}

		switch r {
		case '\\':
			output.WriteString(`\\`)
		case '\n':
			output.WriteString(`\n`)
		case '\r':
			output.WriteString(`\r`)
		case '\t':
			output.WriteString(`\t`)
		default:
			switch {
			case r < 0x20 || r == 0x7f:
				fmt.Fprintf(&output, `\x%02x`, r)
			case unicode.IsControl(r), unicode.Is(unicode.Cf, r), r == '\u2028', r == '\u2029':
				if r <= 0xffff {
					fmt.Fprintf(&output, `\u%04x`, r)
				} else {
					fmt.Fprintf(&output, `\U%08x`, r)
				}
			default:
				output.WriteRune(r)
			}
		}
		value = value[size:]
	}
	return output.String()
}
