package app

import (
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

const maxDiffLines = 6000

func unifiedDiff(before, after, beforeName, afterName string) string {
	beforeLines := boundedLines(before)
	afterLines := boundedLines(after)
	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A: beforeLines, B: afterLines, FromFile: beforeName, ToFile: afterName, Context: 3,
	})
	if err != nil {
		return "无法生成 Diff"
	}
	if strings.TrimSpace(diff) == "" {
		return ""
	}
	return diff
}

func boundedLines(value string) []string {
	if value == "" {
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	if len(lines) > maxDiffLines {
		lines = append(lines[:maxDiffLines], "… 内容超过 6000 行，Diff 已截断 …")
	}
	for index := range lines {
		if !strings.HasSuffix(lines[index], "\n") {
			lines[index] += "\n"
		}
	}
	return lines
}
