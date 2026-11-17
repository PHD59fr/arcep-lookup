package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

func runeLen(text string) int {
	return utf8.RuneCountInString(text)
}

func padRight(text string, width int) string {
	padding := width - runeLen(text)
	if padding <= 0 {
		return text
	}
	return text + strings.Repeat(" ", padding)
}

func wrapText(text string, width int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}

	var lines []string
	current := ""
	for _, word := range words {
		for runeLen(word) > width {
			if current != "" {
				lines = append(lines, current)
				current = ""
			}
			runes := []rune(word)
			lines = append(lines, string(runes[:width]))
			word = string(runes[width:])
		}
		if current == "" {
			current = word
		} else if runeLen(current)+1+runeLen(word) <= width {
			current += " " + word
		} else {
			lines = append(lines, current)
			current = word
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func renderTable(lookup Lookup, database *Database) string {
	rows := [][2]string{{"Number", formatNumber(lookup.Number)}}

	if lookup.Allocation != nil {
		allocation := lookup.Allocation
		rows = append(rows, [2]string{"Assigned operator", allocation.Brand()})
		if allocation.Identity != allocation.Brand() {
			rows = append(rows, [2]string{"ARCEP identity", allocation.Identity})
		}
		rows = append(rows,
			[2]string{"ARCEP code", allocation.Code},
			[2]string{"Type", numberType(lookup.Number)},
			[2]string{"Territory", allocation.Territory},
		)
		if allocation.Start == allocation.End {
			rows = append(rows, [2]string{"Allocated code", formatNumber(allocation.Start)})
		} else {
			rows = append(rows, [2]string{
				"Allocated range",
				formatNumber(allocation.Start) + " - " + formatNumber(allocation.End),
			})
		}
		rows = append(rows,
			[2]string{"Allocation date", allocation.Date},
			[2]string{"Status", "Allocated"},
		)
	} else {
		rows = append(rows,
			[2]string{"Assigned operator", "-"},
			[2]string{"Type", numberType(lookup.Number)},
			[2]string{"Status", "Not allocated"},
		)
		if lookup.Frozen != nil {
			rows = append(rows, [2]string{
				"Detail", "Frozen resource: " + lookup.Frozen.Kind,
			})
		} else {
			rows = append(rows, [2]string{
				"Detail", "No ARCEP resource is allocated to this number.",
			})
		}
	}

	labelWidth := 0
	valueWidth := 20
	for _, row := range rows {
		if width := runeLen(row[0]); width > labelWidth {
			labelWidth = width
		}
		if width := runeLen(row[1]); width > valueWidth {
			valueWidth = width
		}
	}
	if valueWidth > 64 {
		valueWidth = 64
	}

	var builder strings.Builder
	fmt.Fprintf(&builder, "  ARCEP result: %s\n", formatNumber(lookup.Number))
	fmt.Fprintf(
		&builder,
		"┌%s┬%s┐\n",
		strings.Repeat("─", labelWidth+2),
		strings.Repeat("─", valueWidth+2),
	)
	for _, row := range rows {
		valueLines := wrapText(row[1], valueWidth)
		for index, line := range valueLines {
			label := ""
			if index == 0 {
				label = row[0]
			}
			fmt.Fprintf(
				&builder,
				"│ %s │ %s │\n",
				padRight(label, labelWidth),
				padRight(line, valueWidth),
			)
		}
	}
	fmt.Fprintf(
		&builder,
		"└%s┴%s┘\n",
		strings.Repeat("─", labelWidth+2),
		strings.Repeat("─", valueWidth+2),
	)
	fmt.Fprintf(
		&builder,
		"  Source: ARCEP, MAJNUM.csv, data as of %s.\n",
		database.dataDateLabel(),
	)
	if lookup.Allocation != nil && len(lookup.Number) == 10 {
		builder.WriteString("  The ARCEP allocation may differ from the current operator (number portability).\n")
	}
	return builder.String()
}

func renderCompactOneline(lookup Lookup) string {
	if lookup.Allocation != nil {
		allocation := lookup.Allocation
		return fmt.Sprintf("%s  %s (%s)", lookup.Number, allocation.Brand(), allocation.Code)
	}
	if lookup.Frozen != nil {
		return fmt.Sprintf("%s  not allocated (frozen: %s)", lookup.Number, lookup.Frozen.Kind)
	}
	return lookup.Number + "  not allocated"
}

// renderOneline produces the deliberately short default line: a number and
// its assigned operator. Details (type, territory, range, date) are only
// shown with --full.
func renderOneline(lookup Lookup, full bool) string {
	if !full {
		return renderCompactOneline(lookup)
	}

	if lookup.Allocation != nil {
		allocation := lookup.Allocation
		operator := fmt.Sprintf("%s (%s)", allocation.Brand(), allocation.Code)
		if allocation.Identity != allocation.Brand() {
			operator += " - " + allocation.Identity
		}
		rangeLabel := formatNumber(allocation.Start)
		if allocation.Start != allocation.End {
			rangeLabel += " - " + formatNumber(allocation.End)
		}
		return strings.Join([]string{
			lookup.Number,
			operator,
			numberType(lookup.Number),
			allocation.Territory,
			rangeLabel,
			allocation.Date,
		}, " | ")
	}

	status := "not allocated"
	if lookup.Frozen != nil {
		status = "not allocated (frozen: " + lookup.Frozen.Kind + ")"
	}
	return strings.Join([]string{
		lookup.Number,
		status,
		numberType(lookup.Number),
		"-",
		"-",
		"-",
	}, " | ")
}

func formatSize(size int64) string {
	switch {
	case size >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
	case size >= 1024:
		return fmt.Sprintf("%.0f KB", float64(size)/1024)
	default:
		return fmt.Sprintf("%d B", size)
	}
}

func printStatus(database *Database) {
	fmt.Println("Local ARCEP data")
	fmt.Printf("  Directory          %s\n", database.dataDir)

	entries := []struct {
		name        string
		description string
	}{
		{"MAJNUM.csv", fmt.Sprintf("%d allocated ranges", database.AllocCount)},
		{"GELNUM.csv", fmt.Sprintf("%d frozen number prefixes", database.FrozenCount)},
		{"identifiants_CE.csv", fmt.Sprintf("%d operator identities", database.OperatorCount)},
	}
	for _, entry := range entries {
		size := int64(-1)
		if meta, ok := database.metadata.Files[entry.name]; ok && meta.Bytes > 0 {
			size = int64(meta.Bytes)
		} else if info, err := os.Stat(filepath.Join(database.dataDir, entry.name)); err == nil {
			size = info.Size()
		}
		suffix := ""
		if size >= 0 {
			suffix = " - " + formatSize(size)
		}
		fmt.Printf("  %-19s %s%s\n", entry.name, entry.description, suffix)
	}
	fmt.Printf("  MAJNUM data        %s\n", database.dataDateLabel())
	fmt.Printf("  Local update       %s\n", database.updatedAtLabel())
}
