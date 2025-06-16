package main

import (
	"fmt"
	"github.com/gocolly/colly"
	"net/mail"
	"regexp"
	"strings"
	"time"
)

func ExtractEmailWithColly(url string, name string) (string, int, error) {
	c := colly.NewCollector(
		colly.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"),
	)

	c.Limit(&colly.LimitRule{
		DomainGlob:  "*",
		Delay:       2 * time.Second,
		RandomDelay: 1 * time.Second,
	})

	emailPattern := regexp.MustCompile(`(?i)\b[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}\b`)
	allEmails := make(map[string]bool)
	var bestEmail string
	highestScore := -1

	firstName, middleName, lastName := extractNameParts(name)
	blacklist := []string{
		"info@", "contact@", "webmaster@", "noreply@", "support@",
		"enquiries@", "communications@", "press@", "postmaster@",
		"maintainer@", "marketing@", "speaking@", "partnering@",
	}

	checkAndAddEmail := func(email string) {
		clean := sanitizeEmail(email)
		if clean == "" {
			return
		}
		cleanLower := strings.ToLower(clean)

		// Blacklist-Check
		for _, b := range blacklist {
			if strings.Contains(cleanLower, b) {
				return
			}
		}

		score := getScore(cleanLower, firstName, middleName, lastName)

		allEmails[clean] = true
		if score > highestScore {
			bestEmail = clean
			highestScore = score
		}
	}

	c.OnHTML("body", func(e *colly.HTMLElement) {
		// 1. Normale E-Mail-Erkennung im Text
		for _, match := range emailPattern.FindAllString(e.Text, -1) {
			checkAndAddEmail(match)
		}

		// 2. E-Mail-Fragmente zusammensetzen (z. B. Wolfgang.PREE<span>@sbg.ac.at</span>)
		rawHTML, _ := e.DOM.Html()
		fragmentedEmailPattern := regexp.MustCompile(`([a-zA-Z0-9._%+\-]+)<span[^>]*?>.*?</span>@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
		for _, match := range fragmentedEmailPattern.FindAllString(rawHTML, -1) {
			clean := stripHTMLTags(match)
			clean = strings.ReplaceAll(clean, "\n", "")
			checkAndAddEmail(clean)
		}

		// 3. MS Word / MsoNormal Varianten direkt im HTML
		altEmailPattern := regexp.MustCompile(`(?i)([a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,})`)
		for _, match := range altEmailPattern.FindAllString(rawHTML, -1) {
			checkAndAddEmail(match)
		}
	})

	// mailto: links
	c.OnHTML("a[href^='mailto:']", func(e *colly.HTMLElement) {
		text := e.Text
		href := strings.TrimPrefix(e.Attr("href"), "mailto:")

		sources := []string{href, text}
		for _, src := range sources {
			for _, match := range emailPattern.FindAllString(src, -1) {
				checkAndAddEmail(match)
			}
		}
	})

	err := c.Visit(url)
	if err != nil {
		return "", 0, err
	}

	// Fallback: gib erste gültige E-Mail zurück
	if bestEmail == "" {
		for email := range allEmails {
			bestEmail = email
			highestScore = 0
			break
		}
	}

	if bestEmail == "" {
		return "", 0, fmt.Errorf("keine E-Mail extrahiert")
	}

	return bestEmail, highestScore, nil
}

// Entfernt unerwünschte Zeichen & schneidet nach TLD sauber ab
func sanitizeEmail(raw string) string {
	clean := strings.TrimSpace(raw)
	clean = strings.ReplaceAll(clean, "%40", "@")
	clean = strings.TrimRight(clean, ".;, \t\n\r\"'›»")

	if strings.Count(clean, "@") != 1 || strings.Contains(clean, " ") {
		return ""
	}

	idx := strings.Index(clean, "@")
	if idx == -1 || strings.ContainsAny(clean[:idx], "0123456789") {
		return ""
	}

	// Schneide nach offizieller TLD sauber ab (falls nötig)
	clean = truncateEmailAfterTLD(clean)

	// Validierung mit net/mail
	if _, err := mail.ParseAddress(clean); err != nil {
		return ""
	}

	return clean
}

// Trunkiert alles nach einer gültigen Domain-Endung wie .edu, .it etc.
func truncateEmailAfterTLD(email string) string {
	tldPattern := regexp.MustCompile(`(?i)(@[\w\.\-]+\.(edu|com|org|net|gov|ca|de|uk|au|ch|fr|be|it|nl))`)
	loc := tldPattern.FindStringIndex(email)
	if loc != nil {
		return email[:loc[1]]
	}
	return email
}

func stripHTMLTags(input string) string {
	re := regexp.MustCompile(`<[^>]*>`)
	return re.ReplaceAllString(input, "")
}
