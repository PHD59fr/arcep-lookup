package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

var dataURLs = map[string]string{
	"MAJNUM.csv":          "https://extranet.arcep.fr/uploads/MAJNUM.csv",
	"GELNUM.csv":          "https://extranet.arcep.fr/uploads/GELNUM.csv",
	"identifiants_CE.csv": "https://extranet.arcep.fr/uploads/identifiants_CE.csv",
}

var updateOrder = []string{"MAJNUM.csv", "GELNUM.csv", "identifiants_CE.csv"}

var rowLabels = map[string]string{
	"MAJNUM.csv":          "allocated ranges",
	"GELNUM.csv":          "frozen resources",
	"identifiants_CE.csv": "operator identities",
}

type FileMetadata struct {
	Rows               int    `json:"rows"`
	Bytes              int    `json:"bytes"`
	ServerLastModified string `json:"server_last_modified,omitempty"`
}

type Metadata struct {
	UpdatedAt string                  `json:"updated_at"`
	Files     map[string]FileMetadata `json:"files"`
}

type pendingFile struct {
	data         []byte
	rows         int
	lastModified string
}

type stagedFile struct {
	temp        string
	destination string
}

func loadMetadata(dataDir string) Metadata {
	metadata := Metadata{Files: make(map[string]FileMetadata)}
	data, err := os.ReadFile(filepath.Join(dataDir, "metadata.json"))
	if err != nil {
		return metadata
	}
	var loaded Metadata
	if err := json.Unmarshal(data, &loaded); err != nil {
		return metadata
	}
	if loaded.Files == nil {
		loaded.Files = make(map[string]FileMetadata)
	}
	return loaded
}

func parseRFC3339(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}

func writeAtomic(dataDir, name string, data []byte) error {
	temp, err := os.CreateTemp(dataDir, "."+name+".tmp-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			os.Remove(tempName)
		}
	}()

	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempName, filepath.Join(dataDir, name)); err != nil {
		return err
	}
	removeTemp = false
	return nil
}

func updateData(dataDir string) error {
	fmt.Fprintln(os.Stderr, "Updating from the ARCEP extranet…")

	client := &http.Client{Timeout: 120 * time.Second}
	pending := make(map[string]pendingFile, len(updateOrder))

	for _, name := range updateOrder {
		url := dataURLs[name]
		request, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("%s : %w", name, err)
		}
		request.Header.Set("User-Agent", "arcep-go/"+version)

		response, err := client.Do(request)
		if err != nil {
			return fmt.Errorf("downloading %s: %w", name, err)
		}
		payload, readErr := io.ReadAll(io.LimitReader(response.Body, 64<<20))
		response.Body.Close()
		if readErr != nil {
			return fmt.Errorf("reading %s: %w", name, readErr)
		}
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("%s : HTTP %s", name, response.Status)
		}

		rows, err := readCSV(name, payload)
		if err != nil {
			return fmt.Errorf("%s: invalid downloaded data: %w", name, err)
		}
		if len(rows) == 0 {
			return fmt.Errorf("%s contains no rows", name)
		}

		lastModified := ""
		if header := response.Header.Get("Last-Modified"); header != "" {
			if parsed, err := http.ParseTime(header); err == nil {
				lastModified = parsed.UTC().Format(time.RFC3339)
			}
		}
		pending[name] = pendingFile{
			data:         payload,
			rows:         len(rows),
			lastModified: lastModified,
		}
	}

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", dataDir, err)
	}

	var staged []stagedFile
	cleanup := true
	defer func() {
		if cleanup {
			for _, file := range staged {
				os.Remove(file.temp)
			}
		}
	}()

	for _, name := range updateOrder {
		file := pending[name]
		temp, err := os.CreateTemp(dataDir, "."+name+".tmp-*")
		if err != nil {
			return fmt.Errorf("writing %s: %w", name, err)
		}
		tempName := temp.Name()
		staged = append(staged, stagedFile{
			temp:        tempName,
			destination: filepath.Join(dataDir, name),
		})
		if _, err := temp.Write(file.data); err != nil {
			temp.Close()
			return fmt.Errorf("writing %s: %w", name, err)
		}
		if err := temp.Close(); err != nil {
			return fmt.Errorf("writing %s: %w", name, err)
		}
	}
	for _, file := range staged {
		if err := os.Rename(file.temp, file.destination); err != nil {
			return fmt.Errorf("replacing %s: %w", filepath.Base(file.destination), err)
		}
	}
	cleanup = false

	metadata := Metadata{
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		Files:     make(map[string]FileMetadata, len(pending)),
	}
	for name, file := range pending {
		metadata.Files[name] = FileMetadata{
			Rows:               file.rows,
			Bytes:              len(file.data),
			ServerLastModified: file.lastModified,
		}
	}
	encoded, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding metadata: %w", err)
	}
	encoded = append(encoded, '\n')
	if err := writeAtomic(dataDir, "metadata.json", encoded); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: metadata not written (%v).\n", err)
	}

	fmt.Println("Data updated:")
	for _, name := range updateOrder {
		file := pending[name]
		suffix := ""
		if file.lastModified != "" {
			if parsed, err := parseRFC3339(file.lastModified); err == nil {
				suffix = " - data as of " + parsed.Format("02/01/2006")
			}
		}
		fmt.Printf("  %-22s %7d %s%s\n", name, file.rows, rowLabels[name], suffix)
	}
	fmt.Printf("Local directory: %s\n", dataDir)
	return nil
}
