package main

import (
	"context"
	"fmt"
	"github.com/chromedp/chromedp"
	"strings"
	"time"
)

// ExtractEmailFromURL ---------------- Öffentliche Hauptfunktion ----------------
// Besucht die URL, sammelt mailto:, sichtbaren Text & HTML, extrahiert robuste E-Mail-Kandidaten
// (inkl. symbolischer Schreibweise) und bewertet sie mit getScoreOrgGeneral.
// 'name' wird als "Name + Organisation" interpretiert (z. B. "Christos Cassandras Boston University").
func ExtractEmailFromURL(url string, name string) (string, int, error) {
	start := time.Now()

	cleanName := cleanQueryNoise(name)
	firstName, middleName, lastName, org := splitNameAndOrgNoLists(cleanName)

	// ChromeDP Context
	ctx, cancel := chromedp.NewContext(context.Background())
	defer cancel()
	ctx, cancel = context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	// 1) mailto:-Links einsammeln
	var attrs []map[string]string
	err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitReady("body"),
		chromedp.AttributesAll(`a[href^="mailto:"]`, &attrs, chromedp.ByQueryAll),
	)
	if err != nil {
		// nicht fatal – wir versuchen trotzdem Body/HTML
	}

	// 2) Body-Text & Body-HTML holen (für normale & symbolische E-Mails)
	var bodyText, bodyHTML string
	_ = chromedp.Run(ctx,
		chromedp.Text("body", &bodyText, chromedp.NodeVisible, chromedp.ByQuery),
		chromedp.OuterHTML("body", &bodyHTML, chromedp.ByQuery),
	)

	highestScore := -1
	bestEmail := ""

	checkCandidate := func(raw string, source string) {
		mail := extractEmailFromText(raw)
		if mail == "" {
			return
		}
		score := getScoreOrgGeneral(strings.ToLower(mail), firstName, middleName, lastName, org)
		if score > highestScore {
			highestScore = score
			bestEmail = mail
			// Optionales Debug:
			// fmt.Printf("  [%s] %s (score=%d)\n", source, mail, score)
		}
	}

	// --- 2.1 mailto: ---
	for _, m := range attrs {
		if href, ok := m["href"]; ok && strings.HasPrefix(strings.ToLower(href), "mailto:") {
			addr := extractAddressFromMailto(href)
			checkCandidate(addr, "mailto")
		}
	}

	// - 2.2 sichtbarer Text: normale E-Mails ---
	for _, m := range reEmailNormal.FindAllString(bodyText, -1) {
		checkCandidate(m, "text-normal")
	}
	// - 2.3 sichtbarer Text: symbolische E-Mails (strikt) ---
	for _, em := range extractSymbolicEmailsStrict(bodyText, org) {
		checkCandidate(em, "text-symbolic")
	}

	// - 2.4 HTML: normale E-Mails ---
	for _, m := range reEmailNormal.FindAllString(bodyHTML, -1) {
		checkCandidate(m, "html-normal")
	}
	// - 2.5 HTML: symbolische E-Mails (strikt) ---
	for _, em := range extractSymbolicEmailsStrict(bodyHTML, org) {
		checkCandidate(em, "html-symbolic")
	}

	duration := time.Since(start)
	if bestEmail == "" {
		return "", 0, fmt.Errorf("keine gültige Adresse extrahiert (%.2fs)", duration.Seconds())
	}
	fmt.Printf("⏱️ [Chromedp] %s: %.2fs\n", name, duration.Seconds())
	return bestEmail, highestScore, nil
}

// ---------------- Extraktion & Validierung ----------------

// mailto: foo@bar → foo@bar (Query-Teil abgeschnitten)
func extractAddressFromMailto(href string) string {
	if !strings.HasPrefix(strings.ToLower(href), "mailto:") {
		return ""
	}
	email := strings.TrimPrefix(href, "mailto:")
	if i := strings.Index(email, "?"); i != -1 {
		email = email[:i]
	}
	return strings.TrimSpace(email)
}
