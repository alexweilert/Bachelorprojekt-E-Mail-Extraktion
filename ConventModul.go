package main

import (
	"encoding/csv"
	"io"
	"log"
	//"net/http"
	"os"
	//"regexp"
	//"strings"
	//"time"
)

func readCSVFile(filePath string) []string {
	f, err := os.Open(filePath)
	if err != nil {
		log.Fatalf("Unable to read input file %s: %v", filePath, err)
	}
	defer f.Close()

	csvReader := csv.NewReader(f)
	var records []string

	for {
		record, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("Skipping malformed line: %v\n", err)
			continue
		}
		// Füge alle Einträge dieser Zeile zum records-Slice hinzu
		records = append(records, record...)
	}
	return records
}

func duckduckSearch(searchTerm string) {

	return
}
