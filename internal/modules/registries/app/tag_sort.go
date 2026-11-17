package app

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"dosh/internal/domain"
)

var tagVersionPattern = regexp.MustCompile(`^[vV]?([0-9]+(?:\.[0-9]+)*)(.*)$`)

type parsedTagVersion struct {
	parts  []uint64
	suffix string
}

func sortTagsDescending(items []domain.ContainerRegistryTag) {
	sort.SliceStable(items, func(left, right int) bool {
		return compareTagNames(items[left].Name, items[right].Name) > 0
	})
}

func compareTagNames(left, right string) int {
	leftVersion, leftVersioned := parseTagVersion(left)
	rightVersion, rightVersioned := parseTagVersion(right)
	if leftVersioned != rightVersioned {
		if leftVersioned {
			return 1
		}
		return -1
	}
	if leftVersioned {
		if compared := compareVersionParts(leftVersion.parts, rightVersion.parts); compared != 0 {
			return compared
		}
		if leftVersion.suffix == "" && rightVersion.suffix != "" {
			return 1
		}
		if leftVersion.suffix != "" && rightVersion.suffix == "" {
			return -1
		}
	}
	return compareNaturalTags(left, right)
}

func parseTagVersion(tag string) (parsedTagVersion, bool) {
	match := tagVersionPattern.FindStringSubmatch(strings.TrimSpace(tag))
	if match == nil {
		return parsedTagVersion{}, false
	}
	rawParts := strings.Split(match[1], ".")
	parts := make([]uint64, 0, len(rawParts))
	for _, rawPart := range rawParts {
		part, err := strconv.ParseUint(rawPart, 10, 64)
		if err != nil {
			return parsedTagVersion{}, false
		}
		parts = append(parts, part)
	}
	return parsedTagVersion{parts: parts, suffix: match[2]}, true
}

func compareVersionParts(left, right []uint64) int {
	length := len(left)
	if len(right) > length {
		length = len(right)
	}
	for index := 0; index < length; index++ {
		var leftPart, rightPart uint64
		if index < len(left) {
			leftPart = left[index]
		}
		if index < len(right) {
			rightPart = right[index]
		}
		if leftPart < rightPart {
			return -1
		}
		if leftPart > rightPart {
			return 1
		}
	}
	return 0
}

func compareNaturalTags(left, right string) int {
	leftIndex, rightIndex := 0, 0
	for leftIndex < len(left) && rightIndex < len(right) {
		leftDigit := isASCIIDigit(left[leftIndex])
		rightDigit := isASCIIDigit(right[rightIndex])
		if leftDigit && rightDigit {
			leftEnd := leftIndex
			for leftEnd < len(left) && isASCIIDigit(left[leftEnd]) {
				leftEnd++
			}
			rightEnd := rightIndex
			for rightEnd < len(right) && isASCIIDigit(right[rightEnd]) {
				rightEnd++
			}
			if compared := compareNumericChunks(left[leftIndex:leftEnd], right[rightIndex:rightEnd]); compared != 0 {
				return compared
			}
			leftIndex = leftEnd
			rightIndex = rightEnd
			continue
		}
		leftChar := asciiLower(left[leftIndex])
		rightChar := asciiLower(right[rightIndex])
		if leftChar < rightChar {
			return -1
		}
		if leftChar > rightChar {
			return 1
		}
		leftIndex++
		rightIndex++
	}
	switch {
	case leftIndex < len(left):
		return 1
	case rightIndex < len(right):
		return -1
	default:
		return 0
	}
}

func compareNumericChunks(left, right string) int {
	left = strings.TrimLeft(left, "0")
	right = strings.TrimLeft(right, "0")
	if left == "" {
		left = "0"
	}
	if right == "" {
		right = "0"
	}
	if len(left) < len(right) {
		return -1
	}
	if len(left) > len(right) {
		return 1
	}
	return strings.Compare(left, right)
}

func isASCIIDigit(value byte) bool {
	return value >= '0' && value <= '9'
}

func asciiLower(value byte) byte {
	if value >= 'A' && value <= 'Z' {
		return value + ('a' - 'A')
	}
	return value
}
