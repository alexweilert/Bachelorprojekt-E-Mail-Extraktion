package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

func ExtractEmailFromURL(url string, name string) (string, error) {
	ctx, cancel := chromedp.NewContext(context.Background())
	defer cancel()

	ctx, cancel = context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	var results []map[string]string

	err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitReady("body"),
		chromedp.AttributesAll(`a[href^="mailto:"]`, &results, chromedp.ByQueryAll),
	)
	if err != nil {
		return "", fmt.Errorf("Seite nicht erreichbar oder kein mailto gefunden: %w", err)
	}

	var hrefs []string
	for _, attrMap := range results {
		if href, ok := attrMap["href"]; ok {
			hrefs = append(hrefs, href)
		}
	}
	
	if len(hrefs) == 0 {
		return "", fmt.Errorf("Keine mailto-Adressen gefunden")
	}

	// Vor-/Nachnamen extrahieren
	firstName, lastName := "", ""
	parts := strings.Fields(strings.ToLower(name))
	if len(parts) > 0 {
		firstName = parts[0]
	}
	if len(parts) > 1 {
		lastName = parts[len(parts)-1]
	}

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

		score := 0
		if strings.Contains(mailLower, firstName) {
			score += 2
		}
		if strings.Contains(mailLower, lastName) {
			score += 2
		}

		fmt.Printf("[GEFUNDEN] %s → Score: %d\n", mail, score)

		if score > highestScore {
			highestScore = score
			bestEmail = mail
		}
	}

	if bestEmail == "" && len(hrefs) > 0 {
		bestEmail = extractAddressFromMailto(hrefs[0])
		fmt.Printf("[FALLBACK] Erste Adresse genommen: %s\n", bestEmail)
	} else {
		fmt.Printf("[AUSGEWÄHLT] Beste Adresse: %s\n", bestEmail)
	}

	if bestEmail == "" {
		return "", fmt.Errorf("Keine gültige Adresse extrahiert")
	}
	return bestEmail, nil
}

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
