package main

import (
	"encoding/csv"
	"os"
	"strings"
)

type PersonEntry struct {
	Name        string
	Institution string
	Hint        string // optional (3. Spalte), z. B. Website-Hinweis
}

// Query baut den Suchstring (kompatibel zum bisherigen Code)
func (p PersonEntry) Query() string {
	name := strings.TrimSpace(p.Name)
	inst := strings.TrimSpace(p.Institution)
	switch {
	case name != "" && inst != "":
		return name + " " + inst
	case name != "":
		return name
	case inst != "":
		return inst
	default:
		return ""
	}
}

func ReadCSV(filepath string) ([]PersonEntry, error) {
	f, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	var entries []PersonEntry
	lineIndex := 0

	for {
		line, err := r.Read()
		if err != nil {
			break
		}
		// Entferne BOM am ersten Feld
		if lineIndex == 0 && len(line) > 0 {
			line[0] = strings.ReplaceAll(line[0], "\uFEFF", "")
		}

		// Normalisieren auf 3 Felder
		for len(line) < 3 {
			line = append(line, "")
		}

		// Heuristik: 1, 2 oder 3 Spalten unterstützen
		switch {
		case len(strings.TrimSpace(line[1])) == 0 && len(strings.TrimSpace(line[2])) == 0:
			// 1-spaltig (alt): alles in Name, Institution leer
			entries = append(entries, PersonEntry{
				Name:        strings.TrimSpace(line[0]),
				Institution: "",
				Hint:        "",
			})
		case len(strings.TrimSpace(line[2])) == 0:
			// 2-spaltig: Name | Institution
			entries = append(entries, PersonEntry{
				Name:        strings.TrimSpace(line[0]),
				Institution: strings.TrimSpace(line[1]),
				Hint:        "",
			})
		default:
			// 3-spaltig: Name | Institution | Hint
			entries = append(entries, PersonEntry{
				Name:        strings.TrimSpace(line[0]),
				Institution: strings.TrimSpace(line[1]),
				Hint:        strings.TrimSpace(line[2]),
			})
		}
		lineIndex++
	}
	return entries, nil
}

func WriteCSV(outputFile string, results []ResultRow) error {
	file, err := os.Create(outputFile)
	if err != nil {
		return err
	}
	defer file.Close()

	w := csv.NewWriter(file)
	defer w.Flush()

	for _, row := range results {
		_ = w.Write([]string{row.Name, row.Email})
	}
	return nil
}
