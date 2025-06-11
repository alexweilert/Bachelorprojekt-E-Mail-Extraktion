package main

import (
	"fmt"
)

func main() {
	inputFile := "list_of_names_and_affiliations.csv"
	outputFile := "results_variante1.csv"

	entries, err := ReadCSV(inputFile)
	if err != nil {
		panic(err)
	}

	results := make(map[string]string)

	fmt.Println("🔢 Anzahl geladener Zeilen:", len(entries))
	for i, entry := range entries {
		fmt.Printf("\n➡️ [%d/%d] Suche nach: %s\n", i+1, len(entries), entry)

		fmt.Println("Searching for:", entry)
		links, err := DuckDuckGoSearch(entry)
		if err != nil || len(links) == 0 {
			fmt.Println("⚠️ DuckDuckGo fehlgeschlagen – Bing wird verwendet.")
			links, err = BingSearch(entry)
		}

		var email string
		for _, link := range links {
			fmt.Println("Untersuche Link:", link)
			email, err = ExtractEmailFromURL(link, entry)
			if err == nil && email != "" {
				break
			}
		}

		// Wenn keine E-Mail gefunden: Fallback mit "email"-Query
		if email == "" {
			fallbackQuery := entry + " email adress"
			fmt.Println("🔁 Starte Fallback-Suche mit:", fallbackQuery)

			fallbackLinks, err := DuckDuckGoSearch(fallbackQuery)
			if err != nil || len(fallbackLinks) == 0 {
				fmt.Println("⚠️ DuckDuckGo-Fallback fehlgeschlagen – Bing wird verwendet.")
				fallbackLinks, err = BingSearch(fallbackQuery)
			}
			if err == nil {
				for _, link := range fallbackLinks {
					fmt.Println("Untersuche Fallback-Link:", link)
					email, err = ExtractEmailFromURL(link, entry)
					if err == nil && email != "" {
						break
					}
				}
			}
		}

		// Wenn weiterhin nichts gefunden: PDF-Alternative
		if email == "" {
			fmt.Println("📄 Versuche PDF-basierte Suche")
			pdfLinks, err := DuckDuckGoPDFSearch(entry)
			if err == nil && len(pdfLinks) > 0 {
				for _, pdfURL := range pdfLinks {
					filename := "temp.pdf"
					err := DownloadPDF(pdfURL, filename)
					if err == nil {
						emails, err := ExtractEmailsFromPDF(filename)
						if err == nil && len(emails) > 0 {
							email = emails[0]
							break
						}
					}
				}
			}
		}

		if email == "" {
			fmt.Printf("Keine passende E-Mail gefunden für: %s\n", entry)
		} else {
			results[entry] = email
			fmt.Printf("Found: %s => %s\n", entry, email)
		}
	}

	err = WriteCSV(outputFile, results)
	if err != nil {
		panic(err)
	}

	fmt.Println("Results written to", outputFile)
}
