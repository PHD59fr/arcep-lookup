package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const version = "1.0.0"

const usage = `arcep - assigned operator for a phone number
        (ARCEP open data, 100% local lookup)

Usage:
  arcep [options] NUMBER...
  arcep [options] -f FILE
  arcep update [--data-dir DIR]
  arcep status [--data-dir DIR]

Options:
  -f, --file FILE      read numbers from a file ("-" for stdin)
      --full           detailed line: type, territory, range, date
      --oneline        one line per number, even for a single number
      --data-dir DIR   data directory (default: $ARCEP_DATA_DIR or data/ next to the binary)
      --version        print the version
  -h, --help           show this help

One number: detailed table. Several numbers: one short line per number
(number and assigned operator).
Without arguments, reads numbers from standard input if it is redirected.
The "update" command downloads the ARCEP CSV files; lookups make no
network requests.
`

type queryResult struct {
	raw    string
	lookup Lookup
	err    error
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "update", "--update", "-update":
			return runUpdate(args[1:])
		case "status", "--status", "-status":
			return runStatus(args[1:])
		case "version", "--version", "-version":
			fmt.Printf("arcep %s\n", version)
			return 0
		case "help", "-h", "--help":
			fmt.Print(usage)
			return 0
		}
	}

	dataDir := defaultDataDir()
	filePath := ""
	oneline := false
	full := false
	var rawNumbers []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "-h", "--help":
			fmt.Print(usage)
			return 0
		case "--version", "-version":
			fmt.Printf("arcep %s\n", version)
			return 0
		case "--oneline", "--compact":
			oneline = true
		case "--full":
			full = true
		case "-f", "--file":
			if i+1 >= len(args) {
				return usageError("option --file: missing file")
			}
			i++
			filePath = args[i]
		case "--data-dir":
			if i+1 >= len(args) {
				return usageError("option --data-dir: missing directory")
			}
			i++
			dataDir = args[i]
		case "--":
			rawNumbers = append(rawNumbers, args[i+1:]...)
			i = len(args)
		default:
			if strings.HasPrefix(arg, "-") {
				return usageError("unknown option: " + arg)
			}
			rawNumbers = append(rawNumbers, arg)
		}
	}

	var numbers []string
	for _, value := range rawNumbers {
		numbers = append(numbers, splitList(value)...)
	}

	if filePath != "" {
		fromFile, err := readNumbersFromFile(filePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 1
		}
		numbers = append(numbers, fromFile...)
	}

	if len(numbers) == 0 && filePath == "" {
		if stdinIsTerminal() {
			fmt.Fprint(os.Stderr, "No number provided.\n\n")
			fmt.Fprint(os.Stderr, usage)
			return 2
		}
		fromStdin, err := readNumbers(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading standard input: %v\n", err)
			return 1
		}
		numbers = append(numbers, fromStdin...)
	}

	if len(numbers) == 0 {
		fmt.Fprintln(os.Stderr, "No number provided.")
		return 2
	}

	database, err := loadDatabase(expandHome(dataDir))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	results := make([]queryResult, 0, len(numbers))
	for _, raw := range numbers {
		number, err := normalizeNumber(raw)
		if err != nil {
			results = append(results, queryResult{raw: raw, err: err})
			continue
		}
		results = append(results, queryResult{raw: raw, lookup: database.Lookup(number)})
	}

	if len(results) == 1 && results[0].err == nil && !oneline && !full {
		fmt.Print(renderTable(results[0].lookup, database))
		return 0
	}

	valid := 0
	for _, result := range results {
		if result.err != nil {
			fmt.Fprintf(os.Stderr, "%s  invalid number (%v)\n", result.raw, result.err)
			continue
		}
		fmt.Println(renderOneline(result.lookup, full))
		valid++
	}
	if valid == 0 {
		return 2
	}
	return 0
}

func runUpdate(args []string) int {
	dataDir := defaultDataDir()
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-h", "--help":
			fmt.Print(usage)
			return 0
		case "--data-dir":
			if i+1 >= len(args) {
				return usageError("option --data-dir: missing directory")
			}
			i++
			dataDir = args[i]
		default:
			return usageError("unknown option for update: " + args[i])
		}
	}
	if err := updateData(expandHome(dataDir)); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}

func runStatus(args []string) int {
	dataDir := defaultDataDir()
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-h", "--help":
			fmt.Print(usage)
			return 0
		case "--data-dir":
			if i+1 >= len(args) {
				return usageError("option --data-dir: missing directory")
			}
			i++
			dataDir = args[i]
		default:
			return usageError("unknown option for status: " + args[i])
		}
	}
	database, err := loadDatabase(expandHome(dataDir))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	printStatus(database)
	return 0
}

func usageError(message string) int {
	fmt.Fprintf(os.Stderr, "Error: %s\n\n", message)
	fmt.Fprint(os.Stderr, usage)
	return 2
}

func defaultDataDir() string {
	if value := os.Getenv("ARCEP_DATA_DIR"); value != "" {
		return value
	}
	if executable, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(executable), "data")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	return "data"
}

func expandHome(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func stdinIsTerminal() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func splitList(value string) []string {
	parts := strings.FieldsFunc(value, func(char rune) bool {
		return char == ',' || char == ';'
	})
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func readNumbersFromFile(path string) ([]string, error) {
	if path == "-" {
		return readNumbers(os.Stdin)
	}
	file, err := os.Open(expandHome(path))
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	defer file.Close()
	return readNumbers(file)
}

func readNumbers(reader io.Reader) ([]string, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	var numbers []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if line == "" {
			continue
		}
		for _, part := range strings.Split(line, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				numbers = append(numbers, part)
			}
		}
	}
	return numbers, nil
}
