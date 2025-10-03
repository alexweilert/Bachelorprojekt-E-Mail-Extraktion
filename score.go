package main

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/agnivade/levenshtein"
	"golang.org/x/net/publicsuffix"
	"golang.org/x/text/transform"
	unorm "golang.org/x/text/unicode/norm" // Alias, um Kollision zu vermeiden
)

// ----------------------------- Regex -----------------------------

var (
	reEmailQuick = regexp.MustCompile(`(?i)^[a-z0-9._%+\-]{1,64}@[a-z0-9.\-]{3,}\.[a-z]{2,}$`)
	reLocalAlpha = regexp.MustCompile(`^[a-z]+$`)
)

// ----------------------------- Public API ------------------------

func getScoreOrgGeneral(email, first, middle, last, org string) int {
	email = strings.ToLower(strings.TrimSpace(email))
	if !reEmailQuick.MatchString(email) {
		return 0
	}

	// Normalisierung (inkl. Diakritika entfernen)
	first = asciiFold(strings.ToLower(strings.TrimSpace(first)))
	middle = asciiFold(strings.ToLower(strings.TrimSpace(middle)))
	last = asciiFold(strings.ToLower(strings.TrimSpace(last)))
	org = asciiFold(strings.ToLower(strings.TrimSpace(org)))

	local, domain := splitEmail(email)
	if local == "" || domain == "" {
		return 0
	}
	localPlain := removeSeparators(asciiFold(local))
	localTokens := splitLocalTokens(asciiFold(local))

	brand := brandFromDomain(domain)
	orgTokens := tokenizeOrg(org)
	acr2 := orgAcr2(orgTokens)

	score := 0

	// F5: Zwei-Token-Order (vorname.nachname / nachname.vorname)
	score += twoTokenOrderBonus(localTokens, first, last)

	// 1) Levenshtein (max +3)
	if nm := first + middle + last; nm != "" {
		d := levenshtein.ComputeDistance(localPlain, nm)
		n := float64(d) / float64(len(nm)+1)
		switch {
		case n < 0.18:
			score += 3
		case n < 0.35:
			score += 2
		case n < 0.55:
			score += 1
		}
	}

	// 2) Name ↔ Local (deterministisch)
	if len(last) >= 4 {
		sub := last[:minInt(6, maxInt(4, len(last)))]
		if strings.Contains(localPlain, sub) {
			score += 5
		}
	}
	prefixScore := maxInt(prefixRunScore(local, first), prefixRunScore(local, last))
	score += prefixScore
	score += namePrefixHitScore(localTokens, first)
	score += namePrefixHitScore(localTokens, last)

	inits := initials(first, middle, last)
	if len(inits) >= 2 {
		seq := initialsSeqScore(localPlain, inits)
		if seq >= 5 {
			score += 6
		} else if seq >= 2 {
			score += 3
		}
	}
	if reLocalAlpha.MatchString(localPlain) && len(localPlain) >= 2 && len(localPlain) <= 4 {
		if acr2 != "" && brand != "" && strings.EqualFold(brand, acr2) {
			score += 5
		}
	}

	// 3) Org ↔ Domain/Local
	if acr2 != "" && brand != "" && strings.EqualFold(brand, acr2) {
		score += 5
	}
	if brand != "" {
		if strings.HasPrefix(localPlain, brand) || strings.HasSuffix(localPlain, brand) {
			score += 3
		}
		for _, t := range localTokens {
			if t == brand {
				score += 3
				break
			}
		}
	}
	if brand != "" && (tokenContainedInBrand(orgTokens, brand) || brandContainedInTokens(brand, orgTokens)) {
		score += 1
	}

	// 4) leichte Negativsignale
	nameHits := 0
	if len(last) >= 4 && strings.Contains(localPlain, last[:4]) {
		nameHits++
	}
	if hasAnyNamePrefix(localTokens, first) {
		nameHits++
	}
	if hasAnyNamePrefix(localTokens, last) {
		nameHits++
	}

	if len(localPlain) >= 13 && nameHits == 0 {
		score -= 2
	}
	if nameHits == 0 {
		score -= 2
	}

	// Optional: MX-Check (nutzt die Implementierung in Utils.go)
	if hasMXFast(domain) {
		score += 1
	} else {
		score -= 1
	}

	if score < 0 {
		score = 0
	}
	if score > 20 {
		score = 20
	}
	return score
}

// ----------------------------- Helper (Name) ---------------------

func hasAnyNamePrefix(localTokens []string, name string) bool {
	if name == "" {
		return false
	}
	p := strings.ToLower(name)
	limit := minInt(4, len(p))
	for plen := 2; plen <= limit; plen++ {
		pref := p[:plen]
		for _, t := range localTokens {
			if strings.HasPrefix(t, pref) {
				return true
			}
		}
	}
	return false
}

func prefixRunScore(local, name string) int {
	if local == "" || name == "" {
		return 0
	}
	li := 0
	matches := 0
	for _, nr := range name {
		for li < len(local) {
			r, w := utf8.DecodeRuneInString(local[li:])
			if r == '.' || r == '_' || r == '-' {
				li += w
				continue
			}
			if unicode.ToLower(r) == unicode.ToLower(nr) {
				matches++
				li += w
			}
			break
		}
		if li >= len(local) {
			break
		}
	}
	switch {
	case matches >= 6:
		return 6
	case matches >= 5:
		return 6
	case matches >= 4:
		return 5
	case matches == 3:
		return 4
	case matches == 2:
		return 3
	case matches == 1:
		return 1
	default:
		return 0
	}
}

func namePrefixHitScore(localTokens []string, name string) int {
	if name == "" {
		return 0
	}
	nr := []rune(name)
	limit := minInt(4, len(nr))
	best := 0
	for plen := 2; plen <= limit; plen++ {
		pref := strings.ToLower(string(nr[:plen]))
		for _, t := range localTokens {
			if strings.HasPrefix(t, pref) {
				switch plen {
				case 4:
					best = maxInt(best, 4)
				case 3:
					best = maxInt(best, 3)
				default:
					best = maxInt(best, 2)
				}
			}
		}
	}
	return best
}

func initialsSeqScore(localPlain string, initials []rune) int {
	if len(initials) == 0 {
		return 0
	}
	for i := range initials {
		initials[i] = unicode.ToLower(initials[i])
	}
	i := 0
	for _, r := range localPlain {
		if r == initials[i] {
			i++
			if i == len(initials) {
				break
			}
		}
	}
	switch i {
	case 3:
		return 7
	case 2:
		return 5
	case 1:
		return 2
	default:
		return 0
	}
}

func initials(first, middle, last string) []rune {
	out := make([]rune, 0, 3)
	if first != "" {
		out = append(out, []rune(first)[0])
	}
	if middle != "" {
		out = append(out, []rune(middle)[0])
	}
	if last != "" {
		out = append(out, []rune(last)[0])
	}
	return out
}

// Zwei-Token-Order-Bonus
func twoTokenOrderBonus(localTokens []string, first, last string) int {
	if len(localTokens) != 2 || (first == "" && last == "") {
		return 0
	}
	a, b := localTokens[0], localTokens[1]
	f2 := prefixLenMatch(a, first)
	l2 := prefixLenMatch(b, last)
	lf := prefixLenMatch(a, last)
	ff := prefixLenMatch(b, first)
	if f2 >= 2 && l2 >= 2 {
		return 3
	} // vorname.nachname
	if lf >= 2 && ff >= 2 {
		return 3
	} // nachname.vorname
	return 0
}

func prefixLenMatch(token, name string) int {
	if token == "" || name == "" {
		return 0
	}
	token = strings.ToLower(token)
	name = strings.ToLower(name)
	max := 4
	if len(name) < max {
		max = len(name)
	}
	for plen := max; plen >= 2; plen-- {
		if strings.HasPrefix(token, name[:plen]) {
			return plen
		}
	}
	return 0
}

// ----------------------------- Helper (Org) ----------------------

func tokenizeOrg(org string) []string {
	org = strings.ToLower(org)
	re := regexp.MustCompile(`[^a-z0-9]+`)
	org = re.ReplaceAllString(org, " ")
	parts := strings.Fields(org)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if len(p) >= 2 {
			out = append(out, p)
		}
	}
	return out
}

func orgAcr2(tokens []string) string {
	if len(tokens) == 0 {
		return ""
	}
	t := tokens
	if len(t) >= 3 {
		t = t[len(t)-3:]
	}
	if len(t) >= 2 {
		t = t[len(t)-2:]
	}
	acr := ""
	for _, x := range t {
		r := []rune(x)
		if len(r) > 0 && ((r[0] >= 'a' && r[0] <= 'z') || (r[0] >= 'A' && r[0] <= 'Z')) {
			acr += strings.ToLower(string(r[0]))
		}
	}
	if len(acr) > 2 {
		acr = acr[:2]
	}
	return acr
}

func brandFromDomain(domain string) string {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if etld1, err := publicsuffix.EffectiveTLDPlusOne(domain); err == nil && etld1 != "" {
		return strings.Split(etld1, ".")[0]
	}
	parts := strings.Split(domain, ".")
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

func tokenContainedInBrand(tokens []string, brand string) bool {
	for _, t := range tokens {
		if strings.Contains(brand, t) || strings.Contains(t, brand) {
			return true
		}
	}
	return false
}
func brandContainedInTokens(brand string, tokens []string) bool {
	for _, t := range tokens {
		if strings.Contains(t, brand) || strings.Contains(brand, t) {
			return true
		}
	}
	return false
}

// ----------------------------- Helper (E-Mail/MX) ----------------

func splitEmail(email string) (local, domain string) {
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

func removeSeparators(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '.' || r == '_' || r == '-' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func splitLocalTokens(local string) []string {
	sep := func(r rune) bool { return r == '.' || r == '_' || r == '-' }
	toks := strings.FieldsFunc(local, sep)
	out := make([]string, 0, len(toks))
	for _, t := range toks {
		t = strings.TrimSpace(t)
		if t != "" {
			out = append(out, strings.ToLower(t))
		}
	}
	return out
}

// ASCII-Folding: Diakritika entfernen
func asciiFold(s string) string {
	t := transform.Chain(unorm.NFD, transform.RemoveFunc(func(r rune) bool {
		return unicode.Is(unicode.Mn, r)
	}), unorm.NFC)
	out, _, _ := transform.String(t, s)
	return out
}
