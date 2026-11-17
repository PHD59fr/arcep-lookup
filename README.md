# Find the operator assigned a French phone number

A **Go** command-line tool that identifies the operator to which ARCEP
allocated a French phone number, using open data stored locally.

The command runs **one-shot**, with no interactive interface.

Lookups read only the CSV files in `data/`: **no phone number is sent to an
external service**. Only the `update` subcommand performs network requests, to
download ARCEP's public data.

> **Important: number portability**
>
> ARCEP data indicates the operator **to which the number range was allocated**,
> not necessarily the current operator.
>
> After portability (landline or mobile), the number may be used by another
> operator. This tool therefore cannot determine with certainty which operator
> a number is currently active with.

## Build

Standard build:

```bash
go build -o arcep .
```

Static binary, with no external dependency (Go standard library only):

```bash
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o arcep .
```

The compiled binary is available as `./arcep`.

## Usage

```bash
# Look up one number (detailed table)
./arcep 0612345678

# Look up several numbers (one short line per number)
./arcep 0612345678 0140000000 0700000000

# Example output:
# 0612345678  SFR (SFR0)
# 0140000000  Orange (FRTE)
# 0700000000  not allocated

# Read numbers from a file
./arcep -f numbers.txt

# Read numbers from standard input
cat numbers.txt | ./arcep

# Update ARCEP data (the only network operation)
./arcep update

# Show local data status
./arcep status

# Force a short line for a single number
./arcep --oneline 0612345678

# Also show type, territory, range and date
./arcep --full 0612345678 0140000000
# 0612345678 | SFR (SFR0) - Société française du radiotéléphone | Mobile | Métropole | 06 12 00 00 00 - 06 12 99 99 99 | 08/12/2011
```

By default, a result line is deliberately short:

```text
number  operator (code)
```

The `--full` option adds the following fields, separated by ` | `:

```text
number | operator (code) - identity (if different) | type | territory | allocated range | allocation date
```

For an unallocated number, the status replaces the operator and unknown fields
are replaced with `-`.

Some values come directly from ARCEP data and remain in French (operator
identities, territories such as `Métropole`, frozen-resource labels, and dates
in `DD/MM/YYYY` format).

**Behavior without arguments:**

- **In an interactive terminal:** shows the help, then exits immediately.
- **With redirected standard input:** reads numbers from `stdin`.

No interactive loop is provided.

## Accepted number formats

The following formats are supported:

```text
0612345678
06 12 34 56 78
6 12 34 56 78
612345678

+33612345678
+33 6 12 34 56 78
+33.6.12.34.56.78
+33 (0)6 12 34 56 78
+33 06 12 34 56 78

0033612345678
0033 6 12 34 56 78
0033.6.12.34.56.78
0033 06 12 34 56 78

33612345678
33 6 12 34 56 78
33.6.12.34.56.78
330612345678
```

The tool also supports:

- **Short numbers** (`1010`, `118218`, etc.).
- **Extended-length mobile numbers** (13 or 14 digits, for example
  `0700500000000`), including in international format
  (`+33700500000000`, `0033700500000000`).
- The `+33`, `0033` and `33` prefixes, with or without the national `0`.
- Spaces, dots, dashes, parentheses and slashes as separators.

Other international country codes are not supported.

## Commands and options

| Command / Option | Description |
| --- | --- |
| `NUMBER...` | Looks up one or more numbers. Detailed table for a single number, one short line per number for several. |
| `-f, --file FILE` | Reads numbers from a file, one per line or several separated by commas. The value `-` means `stdin`. Empty lines and `#` comments are ignored. |
| `--oneline` | Forces a short line, even for a single number. |
| `--full` | Adds the type, territory, allocated range and allocation date. |
| `--data-dir DIR` | Uses a custom data directory. |
| `update` | Downloads and validates the three ARCEP CSV files. |
| `status` | Shows information about local data: row counts, sizes and update date. |
| `--version` | Shows the tool version. |
| `-h, --help` | Shows the help. |

`--compact` is also accepted as an alias for `--oneline`.

### Data directory

By default, data is looked up in the `data/` directory next to the binary; if
that directory does not exist, the `data/` directory in the current working
directory is used.

Another directory can be set with the `ARCEP_DATA_DIR` environment variable or
the `--data-dir` option.

Examples:

```bash
# Use a custom directory
./arcep --data-dir /opt/arcep/data 0612345678

# Set the directory through an environment variable
export ARCEP_DATA_DIR=/opt/arcep/data
./arcep 0612345678
```

## Data sources

The tool relies on three CSV files published by ARCEP.

| File | Content | Source |
| --- | --- | --- |
| `MAJNUM.csv` | Numbering resources allocated to operators | [Download](https://extranet.arcep.fr/uploads/MAJNUM.csv) |
| `GELNUM.csv` | Frozen or pending numbering resources | [Download](https://extranet.arcep.fr/uploads/GELNUM.csv) |
| `identifiants_CE.csv` | Operator code to identity mapping | [Download](https://extranet.arcep.fr/uploads/identifiants_CE.csv) |

**Official dataset:** [Ressources en numérotation téléphonique](https://www.data.gouv.fr/datasets/ressources-en-numerotation-telephonique)

- **Producer:** ARCEP
- **Update frequency:** weekly
- **License:** Licence Ouverte / Open Licence 2.0 (Etalab)

### Updating data

The following command fetches the latest published data:

```bash
./arcep update
```

The update process:

1. Downloads the three CSV files into memory.
2. Checks their headers and content.
3. Validates the files before replacing anything.
4. Replaces local data only after validation.

**If download or validation fails, the previous data is kept.**

Number lookups require no Internet connection once the data has been
downloaded.

## How it works

Lookup relies on a local index built from ARCEP data.

1. **Loading:** reads CSV files, supporting their Latin-1 encoding.
2. **Indexing:** builds a sorted index, organized by number length.
3. **Lookup:** uses binary search to find the matching number range.
4. **Fallback:** checks frozen prefixes when no allocated range matches.
5. **Resolution:** maps the operator code to its identity using
   `identifiants_CE.csv`.

This approach keeps lookups local, with no external API call.

## Tests

Run all tests:

```bash
go test ./...
```

Tests use the local data and **make no network requests**.

## Project layout

```text
.
├── main.go           # command, arguments and input handling
├── data.go           # CSV loading, normalization and lookup
├── render.go         # table, one-line output, data status
├── update.go         # downloads and metadata
├── arcep_test.go     # tests
├── go.mod
├── data/
│   ├── MAJNUM.csv
│   ├── GELNUM.csv
│   ├── identifiants_CE.csv
│   └── metadata.json
└── arcep             # compiled binary
```

## Privacy

The tool is designed to run **entirely locally** when looking up numbers.

- No looked-up number is sent to an external server.
- No third-party API is required.
- Numbering data is read directly from disk.
- Network connections are limited to the `update` command, which downloads
  ARCEP's public files.

The tool is suitable for one-off checks as well as batch lookups, without
sending analyzed numbers to a third party.
