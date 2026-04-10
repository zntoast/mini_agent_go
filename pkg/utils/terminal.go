package utils

import (
	"regexp"
	"unicode"
)

var ansiEscapeRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

const (
	emojiStart = 0x1F300
	emojiEnd   = 0x1FAFF
)

func CalculateDisplayWidth(text string) int {
	cleanText := ansiEscapeRegex.ReplaceAllString(text, "")

	width := 0
	for _, char := range cleanText {
		if unicode.Is(unicode.Mn, char) {
			continue
		}

		codePoint := int(char)

		if emojiStart <= codePoint && codePoint <= emojiEnd {
			width += 2
			continue
		}

		if isEastAsianWide(char) {
			width += 2
		} else {
			width += 1
		}
	}

	return width
}

func isEastAsianWide(char rune) bool {
	switch {
	case char >= 0x1100 && char <= 0x115F:
		return true
	case char >= 0x2E80 && char <= 0x303E:
		return true
	case char >= 0x3040 && char <= 0xA4CF:
		return true
	case char >= 0xAC00 && char <= 0xD7A3:
		return true
	case char >= 0xF900 && char <= 0xFAFF:
		return true
	case char >= 0xFE10 && char <= 0xFE1F:
		return true
	case char >= 0xFE30 && char <= 0xFE6F:
		return true
	case char >= 0xFF00 && char <= 0xFF60:
		return true
	case char >= 0xFFE0 && char <= 0xFFE6:
		return true
	case char >= 0x20000 && char <= 0x2FFFD:
		return true
	case char >= 0x30000 && char <= 0x3FFFD:
		return true
	}
	return false
}

func TruncateWithEllipsis(text string, maxWidth int, ellipsis string) string {
	if maxWidth <= 0 {
		return ""
	}

	currentWidth := CalculateDisplayWidth(text)
	if currentWidth <= maxWidth {
		return text
	}

	plainText := ansiEscapeRegex.ReplaceAllString(text, "")
	ellipsisWidth := CalculateDisplayWidth(ellipsis)

	if maxWidth <= ellipsisWidth {
		return plainText[:maxWidth]
	}

	availableWidth := maxWidth - ellipsisWidth
	truncated := ""
	currentWidth = 0

	for _, char := range plainText {
		charWidth := CalculateDisplayWidth(string(char))
		if currentWidth+charWidth > availableWidth {
			break
		}
		truncated += string(char)
		currentWidth += charWidth
	}

	return truncated + ellipsis
}

func PadToWidth(text string, targetWidth int, align string, fillChar string) string {
	currentWidth := CalculateDisplayWidth(text)

	if currentWidth >= targetWidth {
		return text
	}

	paddingNeeded := targetWidth - currentWidth

	switch align {
	case "left":
		return text + repeatChar(fillChar, paddingNeeded)
	case "right":
		return repeatChar(fillChar, paddingNeeded) + text
	case "center":
		leftPadding := paddingNeeded / 2
		rightPadding := paddingNeeded - leftPadding
		return repeatChar(fillChar, leftPadding) + text + repeatChar(fillChar, rightPadding)
	default:
		return text
	}
}

func repeatChar(char string, count int) string {
	result := ""
	for i := 0; i < count; i++ {
		result += char
	}
	return result
}
