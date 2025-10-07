package main

import (
	"golang.org/x/net/publicsuffix"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
)

// ---------- Normalisierung & Zerlegung ----------
func normalizeASCII(s string) string {
	// Diakritika raus; Umlaute konservativ umschreiben; Interpunktionsrauschen entfernen
	rep := strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "Ä", "ae", "Ö", "oe", "Ü", "ue", "ß", "ss", "’", "", "'", "", "–", "-", "—", "-")
	s = rep.Replace(s)
	t := transform.Chain(norm.NFD, transform.RemoveFunc(func(r rune) bool { return unicode.Is(unicode.Mn, r) }), norm.NFC)
	out, _, _ := transform.String(t, s)
	return strings.ToLower(strings.TrimSpace(out))
}

// Entfernt gängige Suchzusätze (contact/email/address …), damit die Org-Erkennung nicht leidet.
func cleanQueryNoise(s string) string {
	re := regexp.MustCompile(`(?i)\b(contact|email|e-mail|address|kontakt|kontaktadresse)\b`)
	s = re.ReplaceAllString(s, " ")
	s = strings.Join(strings.Fields(s), " ")
	return s
}

// Split "Name + Organisation" ohne Listen/Helper-Regex.
// Idee: Probiere alle Splits (1..max 4 Tokens als Name) und bewerte Name- und Org-Teil rein strukturell.
// Nimm den Split mit dem höchsten Gesamtscore.
func splitNameAndOrg(entry string) (first, middle, last, org string) {
	entry = strings.TrimSpace(entry)
	// einfache Rauschreduktion
	entry = strings.ReplaceAll(entry, ",", " ")
	entry = strings.ReplaceAll(entry, "  ", " ")
	words := strings.Fields(entry)
	n := len(words)
	if n < 2 {
		return "", "", "", entry
	}

	// Hilfsfunktionen nur für diese Funktion (keine Listen!)
	isAlpha := func(s string) bool {
		for _, r := range s {
			if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && r != '.' && r != '-' && r != '\'' {
				return false
			}
		}
		return len(s) > 0
	}
	isCapWord := func(s string) bool {
		if s == "" {
			return false
		}
		runes := []rune(s)
		// „Christos“, „C.“ → ok; „c.“ oder „christos“ → eher org/token
		return (runes[0] >= 'A' && runes[0] <= 'Z')
	}
	nameTokenScore := func(tok string, idx int) int {
		// Form-Score für einen Namenstoken
		s := 0
		if isAlpha(tok) {
			s += 1
		}
		if isCapWord(tok) {
			s += 2
		}
		l := len([]rune(tok))
		switch {
		case l >= 2 && l <= 15:
			s += 1
		}
		// Initiale wie "G." oder "G" in Mittelpositionen belohnen
		if (l == 1 || (l == 2 && strings.HasSuffix(tok, "."))) && idx > 0 {
			s += 1
		}
		return s
	}
	nameScore := func(toks []string) int {
		if len(toks) == 0 {
			return 0
		}
		s := 0
		for i, t := range toks {
			s += nameTokenScore(t, i)
		}
		// 2–3 Tokens sind typischer
		if len(toks) == 2 {
			s += 2
		}
		if len(toks) == 3 {
			s += 2
		}
		if len(toks) == 1 {
			s -= 1
		}
		if len(toks) >= 4 {
			s -= 2
		}
		return s
	}
	orgScore := func(toks []string) int {
		// Generischer Organisations-Score (ohne Liste):
		// - genug Tokens ist gut (≥1)
		// - längere Tokens (≥3) + Mix aus Klein-/Großschreibung ok
		// - zu viele Ziffern/kurze Tokens → leicht negativ
		if len(toks) == 0 {
			return -3
		}
		s := 0
		short, digit := 0, 0
		for _, t := range toks {
			if len(t) >= 3 {
				s += 1
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
			s -= 1
		}
		if digit > 0 {
			s -= 1
		}
		// mehr als 6 Orgtokens ist ungewöhnlich → leicht negativ
		if len(toks) > 6 {
			s -= 1
		}
		return s
	}

	// Kandidaten-Splits prüfen: 1..min(4, n-1) Tokens als Name
	maxName := 4
	if n-1 < maxName {
		maxName = n - 1
	}

	bestK, bestScore := 1, -1<<30
	for k := 1; k <= maxName; k++ {
		ns := nameScore(words[:k])
		os := orgScore(words[k:])
		score := ns + os
		if score > bestScore {
			bestScore = score
			bestK = k
		}
	}

	nameWords := words[:bestK]
	orgWords := words[bestK:]

	// Name in first/middle/last projizieren (max. 3 Tokens)
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

	org = strings.TrimSpace(strings.Join(orgWords, " "))
	return
}

// Listenfreier Split „Name + Organisation“ (bevorzugt 2 Tokens)
func splitNameAndOrgNoLists(entry string) (first, middle, last, org string) {
	entry = strings.TrimSpace(entry)
	entry = strings.ReplaceAll(entry, ",", " ")
	entry = strings.Join(strings.Fields(entry), " ")
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
	isCapWord := func(s string) bool {
		if s == "" {
			return false
		}
		r := []rune(s)[0]
		return r >= 'A' && r <= 'Z'
	}
	nameTokenScore := func(tok string, idx int) int {
		s := 0
		if isAlpha(tok) {
			s += 1
		}
		if isCapWord(tok) {
			s += 2
		}
		l := len([]rune(tok))
		if l >= 2 && l <= 15 {
			s += 1
		}
		// Initiale wie "G." in Mittelpositionen belohnen
		if (l == 1 || (l == 2 && strings.HasSuffix(tok, "."))) && idx > 0 {
			s += 1
		}
		return s
	}
	nameScore := func(toks []string) int {
		if len(toks) == 0 {
			return 0
		}
		s := 0
		for i, t := range toks {
			s += nameTokenScore(t, i)
		}
		// 2 Tokens deutlich, 3 Tokens leicht bevorzugen
		if len(toks) == 2 {
			s += 3
		}
		if len(toks) == 3 {
			s += 1
		}
		if len(toks) == 1 {
			s -= 1
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
		short, digit := 0, 0
		for _, t := range toks {
			if len(t) >= 3 {
				s += 1
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
			s -= 1
		}
		if digit > 0 {
			s -= 1
		}
		if len(toks) > 6 {
			s -= 1
		}
		return s
	}

	maxName := 4
	if n-1 < maxName {
		maxName = n - 1
	}

	bestK, bestScore := 1, -1<<30
	for k := 1; k <= maxName; k++ {
		ns := nameScore(words[:k])
		os := orgScore(words[k:])
		score := ns + os
		// leichte Abwertung von 3-Token-Namen, wenn die nächsten beiden Tokens „groß“ wirken
		if k == 3 && (k+1) < n {
			next1Cap := isCapWord(words[k])
			next2Cap := (k+1 < n) && isCapWord(words[k+1])
			if next1Cap && next2Cap {
				score -= 2
			}
		}
		if score > bestScore {
			bestScore = score
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
	org = strings.TrimSpace(strings.Join(orgWords, " "))
	return
}

// E-Mail streng aus einem String extrahieren (nur den Treffer, nie „geklebten“ Text)
func extractEmailFromText(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "mailto:", "")
	s = strings.Trim(s, "<> \t\n\r\"',.;:[]{}")
	match := reEmailNormal.FindString(s) // nur die Mail, nicht der ganze String
	if match == "" {
		return ""
	}
	match = truncateEmailAfterTLD(match)
	parts := strings.SplitN(match, "@", 2)
	if len(parts) != 2 {
		return ""
	}
	if !reLocalOK.MatchString(parts[0]) {
		return ""
	}
	if !validDomain(parts[1]) {
		return ""
	}
	return match
}

/*
// Zieht die erste valide E-Mail AUS einem String heraus (auch wenn außen noch Text klebt)
func extractEmailFromText(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "mailto:", "")
	s = strings.Trim(s, "<> \t\n\r\"',.;:[]{}")

	re := regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}`)
	match := re.FindString(s) // <— WICHTIG: FindString, nicht MatchString
	if match == "" {
		return ""
	}
	// generisch nach TLD sauber abschneiden
	match = truncateEmailAfterTLD(match)

	// Minimalprüfung (kein Leerzeichen, genau 1 @)
	if strings.Count(match, "@") != 1 || strings.Contains(match, " ") {
		return ""
	}
	return match
}
*/

// Trunkiert alles nach einer gültigen Domain-Endung (mind. 2 Buchstaben).+
func truncateEmailAfterTLD(email string) string {
	tldPattern := regexp.MustCompile(`(?i)(@[a-z0-9.\-]+\.[a-z]{2,})`)
	loc := tldPattern.FindStringIndex(email)
	if loc != nil {
		return email[:loc[1]]
	}
	return email
}

// strikte Domain-Validierung (Labels + eTLD+1)
func validDomain(domain string) bool {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if strings.Count(domain, ".") < 1 {
		return false
	}
	labels := strings.Split(domain, ".")
	for _, L := range labels {
		if L == "" || len(L) > 63 {
			return false
		}
		if !reLabelOK.MatchString(L) {
			return false
		}
		if strings.HasPrefix(L, "-") || strings.HasSuffix(L, "-") {
			return false
		}
	}
	// TLD ≥ 2 Zeichen
	if len(labels[len(labels)-1]) < 2 {
		return false
	}
	// eTLD+1 muss bestimmbar sein (filtert z. B. "ionforsafetyarguments.thisissue")
	if etld1, err := publicsuffix.EffectiveTLDPlusOne(domain); err != nil || etld1 == "" {
		return false
	}
	return true
}

/*
// NEU: stricte Domain-Validierung (generisch)
func validDomain(domain string) bool {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if strings.Count(domain, ".") < 1 {
		return false
	}
	labels := strings.Split(domain, ".")
	for _, L := range labels {
		if L == "" || len(L) > 63 {
			return false
		}
		// nur a–z, 0–9, '-' und nicht mit '-' beginnen/enden
		if !regexp.MustCompile(`^[a-z0-9-]+$`).MatchString(L) {
			return false
		}
		if strings.HasPrefix(L, "-") || strings.HasSuffix(L, "-") {
			return false
		}
	}
	// TLD ≥2 Zeichen
	last := labels[len(labels)-1]
	if len(last) < 2 {
		return false
	}
	// publicsuffix: eTLD+1 ermitteln (hilft gegen "ionforsafetyarguments.thisissue...")
	if etld1, err := publicsuffix.EffectiveTLDPlusOne(domain); err != nil || etld1 == "" {
		return false
	}
	return true
}
*/

// --- very fast MX with cache & tiny timeout (filters nonsense domains) ---
var mxCache sync.Map

func hasMXFast(domain string) bool {
	if v, ok := mxCache.Load(domain); ok {
		return v.(bool)
	}
	ch := make(chan bool, 1)
	go func() {
		_, err := net.LookupMX(domain)
		ch <- (err == nil)
	}()
	var ok bool
	select {
	case ok = <-ch:
	case <-time.After(250 * time.Millisecond):
		ok = false
	}
	mxCache.Store(domain, ok)
	return ok
}

// --- exact brand vs. org acronym (listenfrei) ---
func brandMatchesOrgAcronym(domain string, org string) bool {
	reg := domain
	if etld1, err := publicsuffix.EffectiveTLDPlusOne(domain); err == nil && etld1 != "" {
		reg = etld1
	}
	brand := strings.Split(reg, ".")[0]
	// Acronym aus den letzten 2 Tokens der Org bilden (z. B. "Boston University" -> "bu")
	toks := strings.Fields(strings.ToLower(org))
	if len(toks) >= 3 {
		toks = toks[len(toks)-3:]
	}
	if len(toks) >= 2 {
		toks = toks[len(toks)-2:]
	}
	acr := ""
	for _, t := range toks {
		r := []rune(t)
		if len(r) > 0 && r[0] >= 'a' && r[0] <= 'z' {
			acr += string(r[0])
		}
	}
	if len(acr) > 2 {
		acr = acr[:2]
	}
	return acr != "" && brand == acr
}

// stricter symbolic extractor: needs '.' or 'dot' in raw, forbids 1-char labels,
// and requires MX OR brand≈org acronym.
func extractSymbolicEmailsStrict(raw string, org string) []string {
	// must contain explicit dot indicator
	if !strings.Contains(raw, ".") && !strings.Contains(strings.ToLower(raw), "dot") {
		return nil
	}
	re := regexp.MustCompile(`(?i)([a-z0-9._+\-]{1,64})\s*(?:\(|\[)?\s*at\s*(?:\)|\])?\s*([a-z0-9.\-\s\[\]\(\)]{1,200})`)
	out := []string{}
	for _, m := range re.FindAllStringSubmatch(raw, -1) {
		local := strings.ToLower(strings.TrimSpace(m[1]))
		if !reLocalOK.MatchString(local) {
			continue
		}

		d := strings.ToLower(m[2])
		// normalize dot words -> '.'
		for _, r := range []string{" (dot) ", " dot ", "[dot]", "(dot)", " dot", "dot ", " DOT ", " Dot "} {
			d = strings.ReplaceAll(d, r, ".")
		}
		d = strings.ReplaceAll(d, " ", "")
		d = strings.ReplaceAll(d, "[.]", ".")
		d = strings.ReplaceAll(d, "(.)", ".")
		d = regexp.MustCompile(`\.{2,}`).ReplaceAllString(d, ".")
		d = strings.Trim(d, ".")

		// domain syntax
		if !validDomain(d) {
			continue
		}

		// forbid 1-char labels (kills "ph.d.thesisresearch"-Art)
		bad := false
		for _, L := range strings.Split(d, ".") {
			if len(L) == 1 {
				bad = true
				break
			}
		}
		if bad {
			continue
		}

		// require MX or brand≈org acronym (fast and generic)
		if !(hasMXFast(d) || brandMatchesOrgAcronym(d, org)) {
			continue
		}

		out = append(out, local+"@"+d)
	}
	return out
}
