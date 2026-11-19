package media

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf16"
)

var tokenPattern = regexp.MustCompile(`^[0-9a-f]{8}$`)

func NewFilenameToken() (string, error) {
	var value [4]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

// Filename is shared by downloads and explicit moves; tokens are not global IDs.
func Filename(title, fallback, extension, token string) (string, error) {
	if !tokenPattern.MatchString(token) || extension == "" || !strings.HasPrefix(extension, ".") || len(extension) > 16 || strings.ContainsAny(extension, `<>:"/\|?* `) {
		return "", fmt.Errorf("파일명 토큰 또는 확장자가 올바르지 않습니다")
	}
	return PathComponent(title, PathComponent(fallback, "untitled", 100), 110) + "_" + token + strings.ToLower(extension), nil
}

func PathComponent(value, fallback string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, value)
	value = strings.Trim(strings.Join(strings.Fields(value), " "), " .")
	if value == "" {
		value = fallback
	}
	var out strings.Builder
	units := 0
	for _, r := range value {
		n := utf16.RuneLen(r)
		if n < 0 {
			n = 1
		}
		if units+n > limit {
			break
		}
		out.WriteRune(r)
		units += n
	}
	value = strings.TrimRight(out.String(), " .")
	if value == "" {
		value = fallback
	}
	base, _, _ := strings.Cut(strings.ToUpper(value), ".")
	base = strings.TrimSpace(base)
	characters := []rune(base)
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || base == "CONIN$" || base == "CONOUT$" || (len(characters) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && strings.ContainsRune("123456789¹²³", characters[3])) {
		value = "_" + value
	}
	return value
}
