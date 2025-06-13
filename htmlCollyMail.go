package main

import (
	"fmt"
	"github.com/gocolly/colly"
	"math/rand"
	"net/mail"
	"regexp"
	"strings"
	"time"
)

func ExtractEmailWithColly(url string, name string) (string, int, error) {
	c := colly.NewCollector(
		colly.UserAgent(""),
	)

	c.Limit(&colly.LimitRule{
		DomainGlob:  "*",             // Gilt für alle Domains
		Delay:       2 * time.Second, // Fester Delay von 2 Sekunden
		RandomDelay: 1 * time.Second, // Zufälliger Zusatzdelay von bis zu 1 Sekunde
	})

	userAgents := []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.0 Safari/605.1.15",
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:115.0) Gecko/20100101 Firefox/115.0",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 16_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.2 Mobile/15E148 Safari/604.1",
	}
	rand.Seed(time.Now().UnixNano())

	c.OnRequest(func(r *colly.Request) {
		ua := userAgents[rand.Intn(len(userAgents))]
		r.Headers.Set("User-Agent", ua)
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
		fmt.Printf("[COLLY] %s → Score: %d\n", clean, score)

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
