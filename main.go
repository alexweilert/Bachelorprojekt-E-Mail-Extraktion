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

		bestEmail := ""
		bestScore := -1

		var lastEmail string
		sameEmailStreak := 0
		emailScores := make(map[string]int)

		for _, link := range links {

			fmt.Println("Untersuche Link:", link)
			email, score, err := ExtractEmailFromURL(link, entry)
			if err != nil || email == "" {
				continue
			}

			// Score-Tracking
			if score > emailScores[email] {
				emailScores[email] = score
			}

			// Vergleich mit vorheriger E-Mail
			if email == lastEmail {
				sameEmailStreak++
			} else {
				sameEmailStreak = 1
				lastEmail = email
			}

			// Update best match
			if emailScores[email] > bestScore {
				bestEmail = email
				bestScore = emailScores[email]
			}

			if score >= 5 {
				fmt.Println("[Treffer] Sehr gute Übereinstimmung → abbrechen")
				break
			}

			// ❗Abbruch: zweimal hintereinander identisch
			if sameEmailStreak >= 2 {
				fmt.Printf("[Abbruch] %s zweimal hintereinander gefunden → übernommen.\n", email)
				bestEmail = email // endgültig übernehmen
				break
			}
		}

		// Am Ende:
		if bestEmail == "" || (bestScore <= 4 && sameEmailStreak <= 1) {
			fmt.Printf("❌ Keine passende E-Mail gefunden für: %s\n", entry)
		} else {
			results[entry] = bestEmail
		}

		// Wenn keine E-Mail gefunden: Fallback mit "E-Mail"-Query
		if bestEmail == "" || (bestScore <= 4 && sameEmailStreak <= 1) {
			fallbackQuery := entry + " email address"
			fmt.Println("🔁 Starte Fallback-Suche mit:", fallbackQuery)

			fallbackLinks, err := DuckDuckGoSearch(fallbackQuery)
			if err != nil || len(fallbackLinks) == 0 {
				fmt.Println("⚠️ DuckDuckGo-Fallback fehlgeschlagen – Bing wird verwendet.")
				fallbackLinks, err = BingSearch(fallbackQuery)
			}
			if err == nil {
				for _, link := range fallbackLinks {
					fmt.Println("Untersuche Fallback-Link:", link)
					email, score, err := ExtractEmailFromURL(link, entry)
					if err == nil && email != "" && score > bestScore {
						bestEmail = email
						bestScore = score
						if score >= 5 {
							break
						}
					}
				}
			}
		}

		// 🧵 Colly-Fallback
		if bestEmail == "" || (bestScore <= 4 && sameEmailStreak <= 1) {
			fmt.Println("🔁 Vorletzter Versuch: Colly-basierte E-Mail-Extraktion")
			for _, link := range links {
				fmt.Println("→ Colly prüft:", link)
				email, score, err := ExtractEmailWithColly(link, entry)
				if err == nil && email != "" {
					bestEmail = email
					bestScore = score
					break
				}
			}
		}

		// Als letzte Option: PDF-Alternative
		if bestEmail == "" || (bestScore <= 4 && sameEmailStreak <= 1) {
			fmt.Println("📄 Letzter Versuch PDF-basierte Suche")
			pdfLinks, err := DuckDuckGoPDFSearch(entry)
			if err == nil && len(pdfLinks) > 0 {
				for _, pdfURL := range pdfLinks {
					filename := "temp.pdf"
					err := DownloadPDF(pdfURL, filename)
					if err == nil {
						email, score, err := ExtractEmailsFromPDF(filename, entry)
						if err == nil && email != "" && score > bestScore {
							bestEmail = email
							bestScore = score
							break
						}
					}
				}
			}
		}

		// Ergebnis-Ausgabe
		if bestEmail == "" || (bestScore <= 4 && sameEmailStreak <= 1) {
			fmt.Printf("❌ Keine passende E-Mail gefunden für: %s\n", entry)
		} else {
			results[entry] = bestEmail
			fmt.Printf("✅ Found: %s => %s (Score: %d)\n", entry, bestEmail, bestScore)
		}
	}

	// CSV schreiben
	err = WriteCSV(outputFile, results)
	if err != nil {
		panic(err)
	}

	fmt.Println("✅ Results written to", outputFile)
}
