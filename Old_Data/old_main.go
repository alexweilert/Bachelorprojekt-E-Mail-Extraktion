// main.go
package Old_Data

import (
	"fmt"
	"os"
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
		contactQuery := entry.NameAndInstitution
		fmt.Printf("\n➡️ [%d/%d] Suche nach: %s\n", i+1, len(entries), contactQuery)

		row := ResultRow{Name: entry.NameAndInstitution, Email: ""}

		links, err := DuckDuckGoSearch(contactQuery)
		if err != nil || len(links) == 0 {
			fmt.Println("⚠️ DuckDuckGo fehlgeschlagen, versuche es später erneut.")
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
							break
						}
					}
					if row.Email == "" || (bestScore < 7 && sameEmailStreak <= 1) {
						email, score, err := ExtractEmailFromURL(link, entry.NameAndInstitution)
						if err == nil && email != "" {
							if updateBestEmail(email, score, &lastEmail, &sameEmailStreak, &bestEmail, &bestScore, emailScores) || score >= 5 {
								row.Email = bestEmail
								break
							}
						}
					}
				}
			}
		}

		if row.Email == "" { // <— einfacher Trigger: nur wenn noch nichts gewählt
			pdfQuery := entry.NameAndInstitution + " filetype:pdf"
			pdfLinks, err := DuckDuckGoPDFSearch(pdfQuery)
			if err == nil && len(pdfLinks) > 0 {
				maxPDF := 3
				if len(pdfLinks) > maxPDF {
					pdfLinks = pdfLinks[:maxPDF]
				}

				for _, pdfURL := range pdfLinks {
					// eindeutige Temp-Datei
					tmp, err := os.CreateTemp("", "emailpdf_*.pdf")
					if err != nil {
						continue
					}
					tmp.Close()
					defer os.Remove(tmp.Name())

					if err := DownloadPDF(pdfURL, tmp.Name()); err != nil {
						continue
					}
					email, score, err := ExtractEmailsFromPDF(tmp.Name(), entry.NameAndInstitution)
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
		//
		if row.Email == "" {
			pdfQuery := entry.NameAndInstitution + " filetype:pdf"
			pdfLinks, err := DuckDuckGoPDFSearch(pdfQuery)
			if err == nil && len(pdfLinks) > 0 {
				for _, pdfURL := range pdfLinks {
					filename := "temp.pdf"
					err := DownloadPDF(pdfURL, filename)
					if err == nil {
						email, score, err := ExtractEmailsFromPDF(filename, entry.NameAndInstitution)
						if err == nil && email != "" {
							// überall, wo du Kandidaten verarbeitest:
							if updateBestEmail(email, score, &lastEmail, &sameEmailStreak, &bestEmail, &bestScore, emailScores) || score >= 5 {
								row.Email = bestEmail
								break
							}
						}
					}
				}
			}
		} // … vor jeder nächsten Phase (Chromedp, PDF, …):
		results = append(results, row)
		if row.Email != "" {
			foundCount++
			fmt.Printf("✅ Found: %s => %s (Score: %d)\n", entry.NameAndInstitution, bestEmail, bestScore)
		} else {
			fmt.Printf("❌ Keine passende E-Mail gefunden für: %s, %s, (Score: %d)\n", entry.NameAndInstitution, bestEmail, bestScore)
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

// ---- kleine Hilfsfunktionen lokal in main.go (keine Listen) ----

func isInitialsLocalPart(local string) bool {
	if local == "" || len(local) < 2 || len(local) > 4 {
		return false
	}
	for _, r := range local {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return true
}

func brandFromEmail(email string) string {
	parts := strings.SplitN(strings.ToLower(email), "@", 2)
	if len(parts) != 2 {
		return ""
	}
	domain := parts[1]
	labels := strings.Split(domain, ".")
	if len(labels) == 0 {
		return ""
	}
	return labels[0] // registrable label (nahe genug)
}

// rein strukturell: Split ohne Listen (1..4 Tokens als Name, Rest = Org)
func splitNameAndOrgNoLists_allgemein(entry string) (first, middle, last, org string) {
	entry = strings.TrimSpace(strings.ReplaceAll(entry, ",", " "))
	words := strings.Fields(entry)
	n := len(words)
	if n < 2 {
		return "", "", "", entry
	}

	isAlpha := func(s string) bool {
		for _, r := range s {
			if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && r != '.' && r != '-' && r != '\'' {
				return false
			}
		}
		return len(s) > 0
	}
	isCap := func(s string) bool {
		if s == "" {
			return false
		}
		r := []rune(s)[0]
		return r >= 'A' && r <= 'Z'
	}
	nameTokScore := func(tok string, idx int) int {
		s := 0
		if isAlpha(tok) {
			s++
		}
		if isCap(tok) {
			s += 2
		}
		l := len([]rune(tok))
		if l >= 2 && l <= 15 {
			s++
		}
		if (l == 1 || (l == 2 && strings.HasSuffix(tok, "."))) && idx > 0 {
			s++
		} // Initiale in Mitte
		return s
	}
	nameScore := func(toks []string) int {
		if len(toks) == 0 {
			return 0
		}
		s := 0
		for i, t := range toks {
			s += nameTokScore(t, i)
		}
		if len(toks) == 2 {
			s += 2
		}
		if len(toks) == 3 {
			s += 2
		}
		if len(toks) == 1 {
			s--
		}
		if len(toks) >= 4 {
			s -= 2
		}
		return s
	}
	orgScore := func(toks []string) int {
		if len(toks) == 0 {
			return -3
		}
		s := 0
		short := 0
		digit := 0
		for _, t := range toks {
			if len(t) >= 3 {
				s++
			}
			for _, r := range t {
				if r >= '0' && r <= '9' {
					digit++
				}
			}
			if len(t) <= 2 {
				short++
			}
		}
		if short > len(toks)/2 {
			s--
		}
		if digit > 0 {
			s--
		}
		if len(toks) > 6 {
			s--
		}
		return s
	}

	maxName := 4
	if n-1 < maxName {
		maxName = n - 1
	}
	bestK, best := 1, -1<<30
	for k := 1; k <= maxName; k++ {
		sc := nameScore(words[:k]) + orgScore(words[k:])
		if sc > best {
			best = sc
			bestK = k
		}
	}
	nameWords := words[:bestK]
	orgWords := words[bestK:]

	switch len(nameWords) {
	case 1:
		first = strings.ToLower(nameWords[0])
	case 2:
		first = strings.ToLower(nameWords[0])
		last = strings.ToLower(nameWords[1])
	default:
		first = strings.ToLower(nameWords[0])
		middle = strings.ToLower(nameWords[1])
		last = strings.ToLower(nameWords[2])
	}
	org = strings.ToLower(strings.Join(orgWords, " "))
	return
}

func initialsAccept(email string, query string) bool {
	// baue Name/Org aus Query
	first, _, last, org := splitNameAndOrgNoLists_allgemein(query)
	if first == "" || last == "" {
		return false
	}

	parts := strings.SplitN(strings.ToLower(email), "@", 2)
	if len(parts) != 2 {
		return false
	}
	local := parts[0]
	if !isInitialsLocalPart(local) {
		return false
	}

	fi := string([]rune(first)[0])
	li := string([]rune(last)[0])

	// Domain-Brand ~ Org-Acronym (letzte 2 Tokens)
	brand := brandFromEmail(email)
	orgToks := strings.Fields(org)
	if len(orgToks) >= 3 {
		orgToks = orgToks[len(orgToks)-2:]
	}
	acr := ""
	for _, t := range orgToks {
		r := []rune(t)
		if len(r) > 0 && r[0] >= 'a' && r[0] <= 'z' {
			acr += string(r[0])
		}
	}
	if len(acr) > 2 {
		acr = acr[:2]
	}

	brandStrong := brand != "" && acr != "" && brand == acr

	// Annahme, wenn: kurzer Buchstaben-Localpart UND (startet mit First-Initial ODER endet mit Last-Initial) UND Domain passt zur Org
	if brandStrong && ((strings.HasPrefix(local, fi)) || (strings.HasSuffix(local, li))) {
		return true
	}
	// oder: startet mit fi UND endet mit li (sehr stark), Domain egal
	if strings.HasPrefix(local, fi) && strings.HasSuffix(local, li) {
		return true
	}
	return false
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
