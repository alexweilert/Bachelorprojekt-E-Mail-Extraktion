package main

import (
	"fmt"
	"github.com/gocolly/colly"
	"regexp"
	"strings"
)

func ExtractEmailWithColly(url, name string) (string, int, error) {
	c := colly.NewCollector(
		colly.UserAgent("Mozilla/5.0"),
	)

	var foundEmail string
	emailPattern := regexp.MustCompile(`(?i)\b[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}\b`)
	allEmails := make(map[string]bool)

	firstName, middleName, lastName := extractNameParts(name)
	highestScore := -1

	c.OnHTML("body", func(e *colly.HTMLElement) {
		matches := emailPattern.FindAllString(e.Text, -1)
		for _, match := range matches {
			email := strings.ToLower(strings.TrimSpace(strings.TrimRight(match, ".;, \t\n\r\"'›»")))

			if strings.Contains(email, " ") || strings.Count(email, "@") != 1 {
				continue
			}
			if idx := strings.Index(email, "@"); idx > 0 {
				if strings.ContainsAny(email[:idx], "0123456789") {
					continue
				}
			}

			score := getScore(email, firstName, middleName, lastName)
			fmt.Printf("[COLLY] %s → Score: %d\n", email, score)
			allEmails[email] = true

			if score > highestScore {
				foundEmail = email
				highestScore = score
			}
		}
	})

	c.OnHTML("a[href^='mailto:']", func(e *colly.HTMLElement) {
		raw := strings.TrimPrefix(e.Attr("href"), "mailto:")
		matches := emailPattern.FindAllString(raw, -1)
		for _, match := range matches {
			email := strings.ToLower(strings.TrimSpace(strings.TrimRight(match, ".;, \t\n\r\"'›»")))
			if strings.Contains(email, " ") || strings.Count(email, "@") != 1 {
				continue
			}
			score := getScore(email, firstName, middleName, lastName)
			fmt.Printf("[COLLY-mailto] %s → Score: %d\n", email, score)
			allEmails[email] = true

			if score > highestScore {
				foundEmail = email
				highestScore = score
			}
		}
	})

	err := c.Visit(url)
	if err != nil {
		return "", 0, err
	}

	// Fallback, falls keine bewertete E-Mail
	if foundEmail == "" {
		for email := range allEmails {
			foundEmail = email
			highestScore = 0
			break
		}
	}

	if foundEmail == "" {
		return "", 0, fmt.Errorf("keine passende E-Mail gefunden (Colly)")
	}

	return foundEmail, highestScore, nil
}
