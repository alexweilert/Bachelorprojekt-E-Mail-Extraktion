package main

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"
)

func ReadCSV(filepath string) ([]string, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(bufio.NewReader(file))
	var lines []string
	lineIndex := 0

	for {
		line, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(line) > 0 {
			// Bereinige BOM aus erster Zeile
			text := strings.TrimSpace(line[0])
			if lineIndex == 0 {
				text = strings.TrimPrefix(text, "\uFEFF")
			}
			lines = append(lines, text)
			lineIndex++
		}
	}
	fmt.Printf("📥 %d Einträge aus CSV geladen\n", len(lines))
	return lines, nil
}

func WriteCSV(outputFile string, results map[string]string) error {
	file, err := os.Create(outputFile)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	for k, v := range results {
		writer.Write([]string{k, v})
	}
	return nil
}
