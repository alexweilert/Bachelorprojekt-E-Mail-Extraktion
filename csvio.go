package main

import (
	"encoding/csv"
	"os"
	"strings"
)

func ReadCSV(filepath string) ([]PersonEntry, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	var entries []PersonEntry
	lineIndex := 0

	for {
		line, err := reader.Read()
		if err != nil {
			break
		}
		if len(line) > 0 {
			text := line[0]
			if lineIndex == 0 {
				text = strings.ReplaceAll(text, "\uFEFF", "")
			}
			entries = append(entries, PersonEntry{NameAndInstitution: text})
			lineIndex++
		}
	}
	return entries, nil
}

type PersonEntry struct {
	NameAndInstitution string
}

func WriteCSV(outputFile string, results []ResultRow) error {
	file, err := os.Create(outputFile)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	writer.Write([]string{"Name + Institution", "Email", "Zeit", "Gefunden auf"})
	for _, row := range results {
		writer.Write([]string{row.Name, row.Email, row.Time, row.Source})
	}
	return nil
}
