package main

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var aliases = map[string]string{
	"FRTE": "Orange",
	"SFR0": "SFR",
	"BOUY": "Bouygues Telecom",
	"FREE": "Free",
	"COMP": "Completel",
}

var requiredColumns = map[string][]string{
	"MAJNUM.csv": {
		"EZABPQM",
		"Tranche_Debut",
		"Tranche_Fin",
		"Mnémo",
		"Territoire",
		"Date_Attribution",
	},
	"GELNUM.csv": {
		"EZABPQM",
		"Type",
	},
	"identifiants_CE.csv": {
		"IDENTITE_OPERATEUR",
		"CODE_OPERATEUR",
	},
}

type Allocation struct {
	Start     string
	End       string
	Code      string
	Identity  string
	Territory string
	Date      string
}

func (a Allocation) Brand() string {
	if brand, ok := aliases[a.Code]; ok {
		return brand
	}
	return a.Identity
}

type FrozenResource struct {
	Prefix string
	Kind   string
}

type Lookup struct {
	Number     string
	Allocation *Allocation
	Frozen     *FrozenResource
}

type lengthIndex struct {
	starts      []int64
	ends        []int64
	allocations []Allocation
}

type Database struct {
	dataDir        string
	metadata       Metadata
	byLength       map[int]*lengthIndex
	frozenByLength map[int]map[string]*FrozenResource
	frozenMaxLen   int

	AllocCount    int
	OperatorCount int
	FrozenCount   int
}

type indexedAllocation struct {
	start int64
	end   int64
	alloc Allocation
}

func decodeCSVData(data []byte) string {
	if utf8.Valid(data) {
		return strings.TrimPrefix(string(data), "\ufeff")
	}
	// ARCEP files are ANSI/ISO-8859-1 encoded.
	runes := make([]rune, len(data))
	for i, b := range data {
		runes[i] = rune(b)
	}
	return string(runes)
}

func readCSV(name string, data []byte) ([]map[string]string, error) {
	text := decodeCSVData(data)
	reader := csv.NewReader(strings.NewReader(text))
	reader.Comma = ';'
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true

	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("unreadable header: %w", err)
	}
	for i := range header {
		header[i] = strings.TrimSpace(strings.TrimPrefix(header[i], "\ufeff"))
	}

	present := make(map[string]bool, len(header))
	for _, column := range header {
		present[column] = true
	}
	for _, column := range requiredColumns[name] {
		if !present[column] {
			return nil, fmt.Errorf("missing column: %s", column)
		}
	}

	var rows []map[string]string
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("unreadable row: %w", err)
		}
		row := make(map[string]string, len(header))
		for i, column := range header {
			if i < len(record) {
				row[column] = strings.TrimSpace(record[i])
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func readCSVFile(path string) ([]map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	rows, err := readCSV(filepath.Base(path), data)
	if err != nil {
		return nil, fmt.Errorf("%s : %w", path, err)
	}
	return rows, nil
}

func loadDatabase(dataDir string) (*Database, error) {
	majPath := filepath.Join(dataDir, "MAJNUM.csv")
	idsPath := filepath.Join(dataDir, "identifiants_CE.csv")
	gelPath := filepath.Join(dataDir, "GELNUM.csv")

	var missing []string
	for _, path := range []string{majPath, idsPath} {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			missing = append(missing, filepath.Base(path))
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf(
			"local data not found (%s in %s)\nRun \"arcep update\" first",
			strings.Join(missing, ", "), dataDir,
		)
	}

	identityRows, err := readCSVFile(idsPath)
	if err != nil {
		return nil, err
	}
	identities := make(map[string]string, len(identityRows))
	for _, row := range identityRows {
		code := row["CODE_OPERATEUR"]
		identity := row["IDENTITE_OPERATEUR"]
		if code != "" && identity != "" {
			identities[code] = identity
		}
	}

	majRows, err := readCSVFile(majPath)
	if err != nil {
		return nil, err
	}
	indexed := make(map[int][]indexedAllocation)
	for _, row := range majRows {
		start := row["Tranche_Debut"]
		end := row["Tranche_Fin"]
		code := row["Mnémo"]
		if code == "" || !isDigits(start) || !isDigits(end) || len(start) != len(end) {
			continue
		}
		startValue, errStart := strconv.ParseInt(start, 10, 64)
		endValue, errEnd := strconv.ParseInt(end, 10, 64)
		if errStart != nil || errEnd != nil || endValue < startValue {
			continue
		}
		identity := identities[code]
		if identity == "" {
			identity = code
		}
		indexed[len(start)] = append(indexed[len(start)], indexedAllocation{
			start: startValue,
			end:   endValue,
			alloc: Allocation{
				Start:     start,
				End:       end,
				Code:      code,
				Identity:  identity,
				Territory: emptyFallback(row["Territoire"], "-"),
				Date:      emptyFallback(row["Date_Attribution"], "-"),
			},
		})
	}

	db := &Database{
		dataDir:        dataDir,
		metadata:       loadMetadata(dataDir),
		byLength:       make(map[int]*lengthIndex, len(indexed)),
		frozenByLength: make(map[int]map[string]*FrozenResource),
	}
	for length, entries := range indexed {
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].start < entries[j].start
		})
		index := &lengthIndex{
			starts:      make([]int64, len(entries)),
			ends:        make([]int64, len(entries)),
			allocations: make([]Allocation, len(entries)),
		}
		for i, entry := range entries {
			index.starts[i] = entry.start
			index.ends[i] = entry.end
			index.allocations[i] = entry.alloc
		}
		db.byLength[length] = index
		db.AllocCount += len(entries)
	}
	db.OperatorCount = len(identities)

	if _, err := os.Stat(gelPath); err == nil {
		gelRows, err := readCSVFile(gelPath)
		if err == nil {
			for _, row := range gelRows {
				prefix := row["EZABPQM"]
				kind := row["Type"]
				if !isDigits(prefix) || kind == "" {
					continue
				}
				// ARCEP frozen-resource labels are in French; keep matching them
				// against the raw data rather than translating the values.
				if !strings.HasPrefix(strings.ToLower(kind), "numéro") {
					continue
				}
				byPrefix := db.frozenByLength[len(prefix)]
				if byPrefix == nil {
					byPrefix = make(map[string]*FrozenResource)
					db.frozenByLength[len(prefix)] = byPrefix
				}
				byPrefix[prefix] = &FrozenResource{Prefix: prefix, Kind: kind}
				db.FrozenCount++
			}
			for length := range db.frozenByLength {
				if length > db.frozenMaxLen {
					db.frozenMaxLen = length
				}
			}
		}
	}

	return db, nil
}

func (db *Database) findAllocation(number string) *Allocation {
	index := db.byLength[len(number)]
	if index == nil {
		return nil
	}
	value, err := strconv.ParseInt(number, 10, 64)
	if err != nil {
		return nil
	}
	position := sort.Search(len(index.starts), func(i int) bool {
		return index.starts[i] > value
	})
	if position == 0 {
		return nil
	}
	position--
	if value <= index.ends[position] {
		return &index.allocations[position]
	}
	return nil
}

func (db *Database) findFrozen(number string) *FrozenResource {
	length := len(number)
	if length <= 6 {
		return db.frozenByLength[length][number]
	}
	if length != 10 && length != 13 && length != 14 {
		return nil
	}

	maxPrefixLength := db.frozenMaxLen
	if maxPrefixLength > length {
		maxPrefixLength = length
	}
	for prefixLength := maxPrefixLength; prefixLength >= 6; prefixLength-- {
		resource := db.frozenByLength[prefixLength][number[:prefixLength]]
		if resource == nil {
			continue
		}
		kind := strings.ToLower(resource.Kind)
		// ARCEP frozen-resource labels are French: "numéro court",
		// "longueur étendue".
		if strings.HasPrefix(kind, "numéro court") {
			continue
		}
		if length >= 13 && !strings.Contains(kind, "longueur étendue") {
			continue
		}
		if length == 10 && strings.Contains(kind, "longueur étendue") {
			continue
		}
		return resource
	}
	return nil
}

func (db *Database) Lookup(number string) Lookup {
	allocation := db.findAllocation(number)
	var frozen *FrozenResource
	if allocation == nil {
		frozen = db.findFrozen(number)
	}
	return Lookup{Number: number, Allocation: allocation, Frozen: frozen}
}

func (db *Database) dataDateLabel() string {
	if file, ok := db.metadata.Files["MAJNUM.csv"]; ok && file.ServerLastModified != "" {
		if t, err := parseRFC3339(file.ServerLastModified); err == nil {
			return t.Format("02/01/2006")
		}
	}
	if info, err := os.Stat(filepath.Join(db.dataDir, "MAJNUM.csv")); err == nil {
		return info.ModTime().Format("02/01/2006")
	}
	return "unknown date"
}

func (db *Database) updatedAtLabel() string {
	if db.metadata.UpdatedAt != "" {
		if t, err := parseRFC3339(db.metadata.UpdatedAt); err == nil {
			return t.Local().Format("02/01/2006 15:04")
		}
	}
	return "unknown date"
}

func emptyFallback(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

var zeroInParentheses = regexp.MustCompile(`\(\s*0\s*\)`)

func normalizeNumber(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	value = strings.ReplaceAll(value, "\u00a0", " ")
	value = zeroInParentheses.ReplaceAllString(value, "")
	value = strings.Map(func(char rune) rune {
		if unicode.IsSpace(char) || strings.ContainsRune(".-–—()/", char) {
			return -1
		}
		return char
	}, value)

	international := false
	switch {
	case strings.HasPrefix(value, "+"):
		if !strings.HasPrefix(value, "+33") {
			return "", errors.New("only the +33 country code is supported")
		}
		value = value[3:]
		international = true
	case strings.HasPrefix(value, "0033"):
		value = value[4:]
		international = true
	case strings.HasPrefix(value, "00"):
		return "", errors.New("only the +33 country code is supported")
	case strings.HasPrefix(value, "33") && isPlainFrenchInternational(value):
		value = value[2:]
		international = true
	}

	if international {
		value = nationalize(value)
	} else if len(value) == 9 {
		// 612345678 → 0612345678
		value = "0" + value
	}

	if value == "" {
		return "", errors.New("empty number")
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return "", errors.New("the number must contain digits only")
		}
	}
	return value, nil
}

// isPlainFrenchInternational recognizes a number prefixed with 33 without
// "+" or "00": 33 + 9 significant digits (mobile/landline), or 33 + 12/13
// significant digits (extended-length mobile numbers). The national 0 kept
// after 33 is also accepted (33 06…).
func isPlainFrenchInternational(value string) bool {
	switch len(value) {
	case 11, 14, 15:
		return true
	case 12, 16:
		return strings.HasPrefix(value, "330")
	default:
		return false
	}
}

// nationalize adds the national 0 when the +33 country code was removed.
// Short numbers (4/6 digits) do not get a 0.
func nationalize(value string) string {
	if value == "" || strings.HasPrefix(value, "0") {
		return value
	}
	if len(value) >= 7 && len(value) <= 13 {
		return "0" + value
	}
	return value
}

func formatNumber(number string) string {
	if len(number) != 10 {
		return number
	}
	return number[0:2] + " " + number[2:4] + " " + number[4:6] + " " +
		number[6:8] + " " + number[8:10]
}

func numberType(number string) string {
	length := len(number)
	if length <= 6 {
		return "Short number"
	}
	if length >= 13 && strings.HasPrefix(number, "07") {
		return "Extended-length mobile"
	}
	if length == 10 {
		switch {
		case strings.HasPrefix(number, "06"), strings.HasPrefix(number, "07"):
			return "Mobile"
		case strings.HasPrefix(number, "09"):
			return "Non-geographic"
		case strings.HasPrefix(number, "08"):
			return "Special / value-added service"
		case number[0] == '0' && strings.ContainsRune("12345", rune(number[1])):
			return "Geographic landline"
		}
	}
	return "Unknown"
}
