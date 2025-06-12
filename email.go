package main

import (
	"context"
	"fmt"
	"github.com/chromedp/chromedp"
	"strings"
	"time"
)

// ExtractEmailFromURL neue Hauptfunktion: gibt E-Mail **und Score** zurück
func ExtractEmailFromURL(url string, name string) (string, int, error) {
	ctx, cancel := chromedp.NewContext(context.Background())
	defer cancel()

	ctx, cancel = context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var results []map[string]string

	err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitReady("body"),
		chromedp.AttributesAll(`a[href^="mailto:"]`, &results, chromedp.ByQueryAll),
	)
	if err != nil {
		return "", 0, fmt.Errorf("seite nicht erreichbar oder kein mailto gefunden: %w", err)
	}

	var hrefs []string
	for _, attrMap := range results {
		if href, ok := attrMap["href"]; ok {
			hrefs = append(hrefs, href)
		}
	}

	if len(hrefs) == 0 {
		return "", 0, fmt.Errorf("keine mailto-Adressen gefunden")
	}

	firstName, middleName, lastName := extractNameParts(name)
	blacklist := []string{"webmaster@", "info@", "contact@", "support@", "noreply@", "maintainer@"}
	highestScore := -1
	var bestEmail string

	for _, attr := range hrefs {
		mail := extractAddressFromMailto(attr)
		mailLower := strings.ToLower(mail)

		if mail == "" || strings.Contains(mail, " ") {
			continue
		}

		skip := false
		for _, b := range blacklist {
			if strings.Contains(mailLower, b) {
				fmt.Printf("[IGNORIERT] %s → Blacklist\n", mail)
				skip = true
				break
			}
		}
		if skip {
			continue
		}

		score := getScore(mailLower, firstName, middleName, lastName)
		fmt.Printf("[GEFUNDEN] %s → Score: %d\n", mail, score)

		if score > highestScore {
			highestScore = score
			bestEmail = mail
		}
	}

	if bestEmail == "" {
		for _, attr := range hrefs {
			mail := extractAddressFromMailto(attr)
			if strings.Contains(mail, "@") {
				domain := strings.SplitN(mail, "@", 2)[1]
				if strings.Contains(domain, "utrc") || strings.Contains(domain, "unitedtech") {
					bestEmail = mail
					highestScore = 0
					fmt.Printf("[FALLBACK mit Domain-Match] %s\n", bestEmail)
					break
				}
			}
		}
		if bestEmail == "" {
			fmt.Println("[KEIN TREFFER] Keine passende E-Mail gefunden.")
			return "", 0, fmt.Errorf("keine gültige Adresse extrahiert")
		}
	} else {
		fmt.Printf("[AUSGEWÄHLT] Beste Adresse: %s (Score: %d)\n", bestEmail, highestScore)
	}

	return bestEmail, highestScore, nil
}

// Extrahiert E-Mail aus mailto:
func extractAddressFromMailto(href string) string {
	if !strings.HasPrefix(href, "mailto:") {
		return ""
	}
	email := strings.TrimPrefix(href, "mailto:")
	if i := strings.Index(email, "?"); i != -1 {
		email = email[:i]
	}
	return strings.TrimSpace(email)
}

func extractNameParts(entry string) (firstName, middleName, lastName string) {
	words := strings.Fields(entry)

	if len(words) < 2 {
		return "", "", ""
	}

	// Heuristik: nimm die ersten 2–3 Tokens als Name, Rest ist Institution
	// Beispiel: "Mohammad Al Faruque University of California Irvine"
	// → Name = "Mohammad Al Faruque", Rest = Uni
	nameEndIndex := 3
	if len(words) < 3 {
		nameEndIndex = len(words)
	}

	nameWords := words[:nameEndIndex]
	firstName = strings.ToLower(nameWords[0])
	lastName = strings.ToLower(nameWords[len(nameWords)-2])
	middleName = strings.ToLower(nameWords[len(nameWords)-1])

	return firstName, middleName, lastName
}
