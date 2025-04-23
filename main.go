package main

import "fmt"

func main() {
	records := readCSVFile("list_of_names_and_affiliations.csv")
	for record := range records {
		fmt.Println(records[record])
		duckduckSearch(records[record])
	}
}
