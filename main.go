// main.go
package main

import (
	"fmt"
	"strings"
	"time"
)

type ResultRow struct {
	Name   string
	Email  string
	Time   string
	Source string
}

func main() {
	inputFile := "list_of_names_and_affiliations.csv"
	outputFile := "results_variante1.csv"

	entries, err := ReadCSV(inputFile)
	if err != nil {
		panic(err)
	}

	var results []ResultRow
	foundCount := 0
	totalStart := time.Now()

	fmt.Println("🔢 Anzahl geladener Zeilen:", len(entries))
	fmt.Println(time.Now().Unix())
	for i, entry := range entries {
		contactQuery := entry.NameAndInstitution + " contact"
		fmt.Printf("\n➡️ [%d/%d] Suche nach: %s\n", i+1, len(entries), contactQuery)
		start := time.Now()

		row := ResultRow{Name: entry.NameAndInstitution, Email: "", Source: ""}

		links, err := DuckDuckGoSearch(contactQuery)
		if err != nil || len(links) == 0 {
			fmt.Println("⚠️ DuckDuckGo fehlgeschlagen, versuche es später erneut.")
			row.Time = fmt.Sprintf("%.2fs", time.Since(start).Seconds())
			results = append(results, row)
			continue
		}

		bestEmail := ""
		bestScore := -1
		lastEmail := ""
		sameEmailStreak := 0
		emailScores := make(map[string]int)

		for _, link := range links {
			email, score, err := ExtractEmailWithColly(link, contactQuery)
			if err != nil || email == "" {
				continue
			}
			if updateBestEmail(email, score, &lastEmail, &sameEmailStreak, &bestEmail, &bestScore, emailScores) || score >= 5 {
				row.Email = bestEmail
				row.Source = link
				break
			}
		}

		if row.Email == "" || (bestScore < 7 && sameEmailStreak <= 1) {
			for _, link := range links {
				email, score, err := ExtractEmailFromURL(link, contactQuery)
				if err != nil || email == "" {
					continue
				}
				if updateBestEmail(email, score, &lastEmail, &sameEmailStreak, &bestEmail, &bestScore, emailScores) || score >= 5 {
					row.Email = bestEmail
					row.Source = link
					break
				}
				if strings.Contains(email, "cgc") {
					fmt.Printf("%s, %d", email, score)
				}
			}
		}

		if row.Email == "" || (bestScore < 7 && sameEmailStreak <= 1) {
			fallbackQuery := entry.NameAndInstitution + " email address"
			fallbackLinks, err := DuckDuckGoSearch(fallbackQuery)
			if err != nil || len(fallbackLinks) == 0 {
				fmt.Println("⚠️ DuckDuckGo-Fallback fehlgeschlagen, versuche es später erneut.")
			} else {
				for _, link := range fallbackLinks {
					email, score, err := ExtractEmailWithColly(link, entry.NameAndInstitution)
					if err == nil && email != "" {
						if updateBestEmail(email, score, &lastEmail, &sameEmailStreak, &bestEmail, &bestScore, emailScores) || score >= 5 {
							row.Email = bestEmail
							row.Source = link
							break
						}
					}
					if row.Email == "" || (bestScore < 7 && sameEmailStreak <= 1) {
						email, score, err := ExtractEmailFromURL(link, entry.NameAndInstitution)
						if err == nil && email != "" {
							if updateBestEmail(email, score, &lastEmail, &sameEmailStreak, &bestEmail, &bestScore, emailScores) || score >= 5 {
								row.Email = bestEmail
								row.Source = link
								break
							}
						}
					}
				}
			}
		}

		if row.Email == "" || (bestScore < 7 && sameEmailStreak <= 1) {
			pdfQuery := entry.NameAndInstitution + " filetype:pdf"
			pdfLinks, err := DuckDuckGoPDFSearch(pdfQuery)
			if err == nil && len(pdfLinks) > 0 {
				for _, pdfURL := range pdfLinks {
					filename := "temp.pdf"
					err := DownloadPDF(pdfURL, filename)
					if err == nil {
						email, score, err := ExtractEmailsFromPDF(filename, entry.NameAndInstitution)
						if err == nil && email != "" {
							if updateBestEmail(email, score, &lastEmail, &sameEmailStreak, &bestEmail, &bestScore, emailScores) || score >= 5 {
								row.Email = bestEmail
								row.Source = pdfURL
								break
							}
						}
					}
				}
			}
		}

		row.Time = fmt.Sprintf("%.2fs", time.Since(start).Seconds())
		results = append(results, row)
		if row.Email != "" {
			foundCount++
			fmt.Printf("✅ Found: %s => %s (Score: %d)\n", entry.NameAndInstitution, bestEmail, bestScore)
		} else {
			fmt.Printf("❌ Keine passende E-Mail gefunden für: %s\n", entry.NameAndInstitution)
		}
	}

	fmt.Println(time.Now().Unix())
	totalDuration := time.Since(totalStart)
	fmt.Printf("⏱️ Gesamtdauer: %.2fs\n", totalDuration.Seconds())

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
