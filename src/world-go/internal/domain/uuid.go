package domain

import "strings"

// isUUID imita Uuid::try_parse del crate uuid que usa Rust (world.rs:810):
// acepta las formas simple (32 dígitos hexadecimales), con guiones (8-4-4-4-12),
// con llaves ({...}) y urn (urn:uuid:...). Los dígitos pueden ir en
// mayúsculas o minúsculas; no se comprueba versión ni variante.
func isUUID(text string) bool {
	switch len(text) {
	case 32:
		return allHex(text)
	case 36:
		return isHyphenatedUUID(text)
	case 38:
		return text[0] == '{' && text[37] == '}' && isHyphenatedUUID(text[1:37])
	case 45:
		return strings.HasPrefix(text, "urn:uuid:") && isHyphenatedUUID(text[9:])
	default:
		return false
	}
}

func isHyphenatedUUID(text string) bool {
	if len(text) != 36 {
		return false
	}
	for _, i := range [...]int{8, 13, 18, 23} {
		if text[i] != '-' {
			return false
		}
	}
	return allHex(text[0:8]) && allHex(text[9:13]) && allHex(text[14:18]) &&
		allHex(text[19:23]) && allHex(text[24:36])
}

func allHex(text string) bool {
	for i := 0; i < len(text); i++ {
		c := text[i]
		isDigit := c >= '0' && c <= '9'
		isLower := c >= 'a' && c <= 'f'
		isUpper := c >= 'A' && c <= 'F'
		if !isDigit && !isLower && !isUpper {
			return false
		}
	}
	return true
}
