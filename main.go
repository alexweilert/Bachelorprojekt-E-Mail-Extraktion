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
		contactQuery := entry + " contact"
		fmt.Printf("\n➡️ [%d/%d] Suche nach: %s\n", i+1, len(entries), contactQuery)

		fmt.Println("Searching for:", contactQuery)
		links, err := DuckDuckGoSearch(contactQuery)
		if err != nil || len(links) == 0 {
			fmt.Println("⚠️ DuckDuckGo fehlgeschlagen, versuche es später erneut.")
		}

		bestEmail := ""
		bestScore := -1
		lastEmail := ""
		sameEmailStreak := 0
		emailScores := make(map[string]int)

		// 1️⃣ Colly zuerst
		for _, link := range links {
			fmt.Println("→ Colly prüft:", link)
			email, score, err := ExtractEmailWithColly(link, contactQuery)
			if err != nil || email == "" {
				continue
			}

			if updateBestEmail(email, score, &lastEmail, &sameEmailStreak, &bestEmail, &bestScore, emailScores) || score >= 5 {
				break
			}
		}

		// 2️⃣ chromedp nur wenn nötig
		if bestEmail == "" || (bestScore <= 4 && sameEmailStreak <= 1) {
			for _, link := range links {
				fmt.Println("→ chromedp prüft:", link)
				email, score, err := ExtractEmailFromURL(link, contactQuery)
				if err != nil || email == "" {
					continue
				}

				if updateBestEmail(email, score, &lastEmail, &sameEmailStreak, &bestEmail, &bestScore, emailScores) || score >= 5 {
					break
				}
			}
		}

		// 3️⃣ Fallback-Suche mit Query
		if bestEmail == "" || (bestScore <= 4 && sameEmailStreak <= 1) {
			fallbackQuery := entry + " email address"
			fmt.Println("🔁 Starte Fallback-Suche mit:", fallbackQuery)

			fallbackLinks, err := DuckDuckGoSearch(fallbackQuery)
			if err != nil || len(fallbackLinks) == 0 {
				fmt.Println("⚠️ DuckDuckGo-Fallback fehlgeschlagen, versuche es später erneut.")
			}
			if err == nil {
				for _, link := range fallbackLinks {
					fmt.Println("Untersuche Fallback-Link:", link)
					email, score, err := ExtractEmailFromURL(link, entry)
					if err == nil && email != "" {
						if updateBestEmail(email, score, &lastEmail, &sameEmailStreak, &bestEmail, &bestScore, emailScores) || score >= 5 {
							break
						}
					}
				}
			}
		}

		// 4️⃣ PDF-Suche als letzte Option
		if bestEmail == "" || (bestScore <= 4 && sameEmailStreak <= 1) {
			fmt.Println("📄 Letzter Versuch PDF-basierte Suche")
			pdfLinks, err := DuckDuckGoPDFSearch(entry)
			if err == nil && len(pdfLinks) > 0 {
				for _, pdfURL := range pdfLinks {
					filename := "temp.pdf"
					err := DownloadPDF(pdfURL, filename)
					if err == nil {
						email, score, err := ExtractEmailsFromPDF(filename, entry)
						if err == nil && email != "" {
							if updateBestEmail(email, score, &lastEmail, &sameEmailStreak, &bestEmail, &bestScore, emailScores) || score >= 5 {
								break
							}
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

	err = WriteCSV(outputFile, results)
	if err != nil {
		panic(err)
	}

	fmt.Println("✅ Results written to", outputFile)
}

func updateBestEmail(
	email string,
	score int,
	lastEmail *string,
	sameEmailStreak *int,
	bestEmail *string,
	bestScore *int,
	emailScores map[string]int,
) bool {

	if score > emailScores[email] {
		emailScores[email] = score
	}

	if email == *lastEmail && score > 0 {
		*sameEmailStreak++
	} else {
		*sameEmailStreak = 1
		*lastEmail = email
	}

	if emailScores[email] > *bestScore {
		*bestEmail = email
		*bestScore = emailScores[email]
	}

	if *sameEmailStreak >= 4 && *bestScore >= 3 {
		fmt.Printf("[Abbruch] %s dreimal hintereinander gefunden → übernommen.\n", email)
		return true
	}

	return false
}
