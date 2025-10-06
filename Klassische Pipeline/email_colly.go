package Klassische_Pipeline

import (
	"fmt"
	"github.com/gocolly/colly"
	"regexp"
	"strings"
	"time"
)

// ExtractEmailWithColly besucht eine URL, extrahiert Kandidaten und bewertet mit getScoreOrgGeneral.
// Der Parameter 'name' wird als "Name + Organisation" interpretiert.
func ExtractEmailWithColly(url string, name string) (string, int, error) {
	start := time.Now()
	c := colly.NewCollector(
		colly.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"),
	)

	c.Limit(&colly.LimitRule{
		DomainGlob:  "*",
		Delay:       2 * time.Second,
		RandomDelay: 1 * time.Second,
	})

	// generisches E-Mail-Muster
	emailPattern := regexp.MustCompile(`(?i)\b[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}\b`)
	allEmails := make(map[string]bool)
	var bestEmail string
	highestScore := -1
	// ——— Name + Organisation heuristisch aus "name" ableiten (robust gg. Suchzusätze) ———
	firstName, middleName, lastName, org := splitNameAndOrg(cleanQueryNoise(name))

	checkAndAddEmail := func(raw string) {
		mail := extractEmailFromText(raw) // <— statt sanitizeEmail(raw)
		if mail == "" {
			return
		}
		score := getScoreOrgGeneral(strings.ToLower(mail), firstName, middleName, lastName, org)

		allEmails[mail] = true
		if score > highestScore {
			bestEmail = mail
			highestScore = score
		}
	}

	c.OnHTML("body", func(e *colly.HTMLElement) {
		// 1) Normale E-Mail-Erkennung im sichtbaren Text
		for _, match := range emailPattern.FindAllString(e.Text, -1) {
			checkAndAddEmail(match)
		}
		// 1b) SYMBOLISCHE Erkennung im sichtbaren Text
		//for _, em := range extractSymbolicEmails(e.Text) {
		for _, em := range extractSymbolicEmailsStrict(e.Text, org) {
			checkAndAddEmail(em)
		}

		// 2) Fragmentierte HTML-Varianten
		rawHTML, _ := e.DOM.Html()
		fragmentedEmailPattern := regexp.MustCompile(`([a-zA-Z0-9._%+\-]+)<span[^>]*?>.*?</span>@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
		for _, match := range fragmentedEmailPattern.FindAllString(rawHTML, -1) {
			stripped := stripHTMLTags(match)
			stripped = strings.ReplaceAll(stripped, "\n", "")
			checkAndAddEmail(stripped)
		}

		// 3) MSO/Alternative
		altEmailPattern := regexp.MustCompile(`(?i)([a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,})`)
		for _, match := range altEmailPattern.FindAllString(rawHTML, -1) {
			checkAndAddEmail(match)
		}
		// 3b) SYMBOLISCHE Erkennung im HTML (falls Text nicht gereicht hat)
		//for _, em := range extractSymbolicEmails(rawHTML) {
		for _, em := range extractSymbolicEmailsStrict(rawHTML, org) {
			checkAndAddEmail(em)
		}
	})

	// 4) mailto:-Links
	c.OnHTML("a[href^='mailto:']", func(e *colly.HTMLElement) {
		text := e.Text
		href := strings.TrimPrefix(e.Attr("href"), "mailto:")
		for _, src := range []string{href, text} {
			// nicht regexpen, sondern robust extrahieren:
			if mail := extractEmailFromText(src); mail != "" {
				checkAndAddEmail(mail)
			}
		}
	})

	if err := c.Visit(url); err != nil {
		return "", 0, err
	}

	// Fallback: erste valide Adresse
	if bestEmail == "" {
		for email := range allEmails {
			bestEmail = email
			highestScore = 0
			break
		}
	}

	fmt.Printf("⏱️ [Colly] %s: %.2fs\n", name, time.Since(start).Seconds())

	if bestEmail == "" {
		return "", 0, fmt.Errorf("keine E-Mail extrahiert")
	}

	return bestEmail, highestScore, nil
}

// NEU: symbolische E-Mails aus freiem Text extrahieren (at/dot-Varianten)
func extractSymbolicEmails(text string) []string {
	// etwas „robustere“ Erkennung: username [at|@] domain [dot|.] tld
	// wir verlangen mind. EINEN Punkt nach dem Zusammensetzen
	reSym := regexp.MustCompile(`(?i)([a-z0-9._+\-]{1,64})\s*(?:\(|\[)?\s*at\s*(?:\)|\])?\s*([a-z0-9.\-\s\[\]\(\)]{1,200})`)
	cands := []string{}
	matches := reSym.FindAllStringSubmatch(text, -1)
	for _, m := range matches {
		local := strings.ToLower(strings.TrimSpace(m[1]))
		rawDomain := strings.ToLower(m[2])
		// " dot " / "(dot)" / "[dot]" / reale Punkte → echte Punkte, Whitespaces raus
		d := rawDomain
		replacements := []string{" (dot) ", " dot ", "[dot]", "(dot)", " dot", "dot "}
		for _, r := range replacements {
			d = strings.ReplaceAll(d, r, ".")
		}
		d = strings.ReplaceAll(d, " ", "")
		d = strings.ReplaceAll(d, "[.]", ".")
		d = strings.ReplaceAll(d, "(.)", ".")
		// minimal säubern (mehrfachpunkte reduzieren)
		d = regexp.MustCompile(`\.{2,}`).ReplaceAllString(d, ".")

		// harte Filter: Local & Domain plausibel?
		if local == "" || len(local) > 64 || !regexp.MustCompile(`^[a-z0-9._+\-]+$`).MatchString(local) {
			continue
		}
		if !validDomain(d) {
			continue
		}
		cands = append(cands, local+"@"+d)
	}
	return cands
}

func stripHTMLTags(input string) string {
	re := regexp.MustCompile(`<[^>]*>`)
	return re.ReplaceAllString(input, "")
}
