package main

import (
	"Bachelorprojekt/Klassische Pipeline"
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

// -------------------- Schwellwerte / Limits --------------------

const (
	maxLinksPhase1   = 10
	maxLinksFallback = 10
	maxLinksPDF      = 6 // weniger PDFs pro Person → stabiler

	hardAcceptScore   = 14 // sehr sicher -> sofort final
	consensusMinScore = 6  // Konsens braucht mind. diesen Score
)

// -------------------- Kandidaten-Aggregation -------------------

type ResultRow struct {
	Name   string
	Email  string
	Time   string
	Source string
}

type candInfo struct {
	bestScore int
	sources   map[string]struct{} // Set verschiedener Quellen (Host+Path)
}

// --------------------------- main ------------------------------

func main() {
	// Eingabedatei (optional via CLI-Arg überschreibbar)
	inputFile := "list_of_names_and_affiliations.csv"
	if len(os.Args) > 1 && strings.TrimSpace(os.Args[1]) != "" {
		inputFile = os.Args[1]
	}

	entries, err := Klassische_Pipeline.ReadCSV(inputFile)
	if err != nil {
		fmt.Printf("Fehler beim Lesen der CSV (%s): %v\n", inputFile, err)
		return
	}
	if len(entries) == 0 {
		fmt.Println("Keine Einträge in der CSV gefunden.")
		return
	}
	fmt.Printf("📄 Eingelesen: %d Einträge aus %s\n", len(entries), inputFile)

	results := make([]ResultRow, 0, len(entries))
	foundCount := 0
	startAll := time.Now()
	rand.Seed(time.Now().UnixNano())

PERSON_LOOP:
	for i, entry := range entries {
		// kleine Höflichkeitspause zwischen Personen
		if i > 0 {
			time.Sleep(time.Duration(1500+rand.Intn(2000)) * time.Millisecond)
		}

		contactQuery := buildQuery(entry)
		if contactQuery == "" {
			continue
		}

		fmt.Printf("\n➡️ [%d/%d] Suche nach: %s\n", i+1, len(entries), contactQuery)

		row := ResultRow{Name: contactQuery, Email: "", Source: ""}

		// Kandidaten sammeln über alle Phasen
		candidates := map[string]*candInfo{}

		// ----------------- Phase 1: DDG → Colly → Chromedp -----------------
		phase1Links, err := Klassische_Pipeline.DuckDuckGoSearch(contactQuery)
		if err != nil {
			fmt.Printf("⚠️ DuckDuckGo fehlgeschlagen: %v\n", err)
			phase1Links = nil
		}
		fmt.Printf("🔎 Phase1: %d Links\n", len(phase1Links))

		if len(phase1Links) > maxLinksPhase1 {
			phase1Links = phase1Links[:maxLinksPhase1]
		}

		// Colly mit Early-Accept
		if finalized, email, src := processLinksCollyEarly(phase1Links, contactQuery, candidates); finalized {
			row.Email, row.Source = email, src
			addResultOnce(&results, row)
			foundCount++
			fmt.Printf("✅ Found (early): %s => %s\n", contactQuery, row.Email)
			continue PERSON_LOOP
		}

		// Chromedp mit Early-Accept
		if finalized, email, src := processLinksChromedpEarly(phase1Links, contactQuery, candidates); finalized {
			row.Email, row.Source = email, src
			addResultOnce(&results, row)
			foundCount++
			fmt.Printf("✅ Found (early): %s => %s\n", contactQuery, row.Email)
			continue PERSON_LOOP
		}

		// ----------------- Fallback: „email address“ -----------------------
		fallbackQuery := contactQuery + " email address"
		fallbackLinks, ferr := Klassische_Pipeline.DuckDuckGoSearch(fallbackQuery)
		if ferr != nil {
			fmt.Printf("⚠️ DuckDuckGo Fallback fehlgeschlagen: %v\n", ferr)
			fallbackLinks = nil
		}
		fmt.Printf("🔎 Fallback: %d Links\n", len(fallbackLinks))
		if len(fallbackLinks) > maxLinksFallback {
			fallbackLinks = fallbackLinks[:maxLinksFallback]
		}

		// Colly mit Early-Accept
		if finalized, email, src := processLinksCollyEarly(fallbackLinks, contactQuery, candidates); finalized {
			row.Email, row.Source = email, src
			addResultOnce(&results, row)
			foundCount++
			fmt.Printf("✅ Found (early): %s => %s\n", contactQuery, row.Email)
			continue PERSON_LOOP
		}

		// Chromedp mit Early-Accept
		if finalized, email, src := processLinksChromedpEarly(fallbackLinks, contactQuery, candidates); finalized {
			row.Email, row.Source = email, src
			addResultOnce(&results, row)
			foundCount++
			fmt.Printf("✅ Found (early): %s => %s\n", contactQuery, row.Email)
			continue PERSON_LOOP
		}

		// ----------------- Phase 2: PDFs -----------------------------------
		pdfQuery := contactQuery + " filetype:pdf"
		pdfLinks, perr := Klassische_Pipeline.DuckDuckGoPDFSearch(pdfQuery)

		// ----- Worker-Mode für sichere PDF-Analyse (Subprozess) -----
		if len(os.Args) > 1 && os.Args[1] == "--scanpdf" {
			// Erwartet: --scanpdf <pdfPath> <person>
			if len(os.Args) < 4 {
				fmt.Println("NONE")
				return
			}
			// Hartes Heap-Limit nur für den Worker (z. B. 200 MiB)
			debug.SetMemoryLimit(200 << 20)
			pdfPath := os.Args[2]
			person := os.Args[3]

			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()

			email, score, err := Klassische_Pipeline.ExtractEmailsFromPDFCtx(ctx, pdfPath, person)
			if err != nil || email == "" {
				fmt.Println("NONE")
				return
			}
			fmt.Printf("OK|%s|%d\n", email, score)
			return
		}

		if perr != nil {
			fmt.Printf("⚠️ DuckDuckGo (PDF) Fehler: %v\n", perr)
			pdfLinks = nil
		}
		fmt.Printf("🔎 PDF: %d Links\n", len(pdfLinks))
		if len(pdfLinks) > maxLinksPDF {
			pdfLinks = pdfLinks[:maxLinksPDF]
		}

		for _, pdfURL := range pdfLinks {
			// leichte Pause zwischen PDFs, um Blockaden zu vermeiden
			time.Sleep(time.Duration(3000+rand.Intn(1500)) * time.Millisecond)
			tmp, terr := os.CreateTemp("", "emailpdf_*.pdf")
			if terr != nil {
				continue
			}
			tmp.Close()
			defer os.Remove(tmp.Name())
			if derr := Klassische_Pipeline.DownloadPDF(pdfURL, tmp.Name()); derr != nil {
				continue
			}
			start := time.Now()
			// Subprozess mit hartem Timeout pro PDF
			ctxPDF, cancelPDF := context.WithTimeout(context.Background(), 12*time.Second)
			email, score, werr := scanPDFInSubprocess(ctxPDF, tmp.Name(), contactQuery)
			cancelPDF()
			fmt.Printf("⏱️ [PDF fast] %s: %.2fs\n", contactQuery, time.Since(start).Seconds())
			if werr != nil {
				// Worker-Timeout/Crash → einfach nächste PDF
				fmt.Printf("⏭️ Skip PDF (worker err: %v)\n", werr)
				continue
			}
			if email == "" {
				continue
			}

			registerCandidate(candidates, email, score, pdfURL)
			// Early-Accept in PDF-Phase
			if shouldEarlyAccept(candidates, email, score) {
				row.Email, row.Source = email, pdfURL
				addResultOnce(&results, row)
				foundCount++
				fmt.Printf("✅ Found (early): %s => %s\n", contactQuery, row.Email)
				continue PERSON_LOOP
			}
		}

		// ----------------- Finale Auswahl (wenn kein Early-Accept) ----------
		finalEmail, finalSource := pickFinal(candidates, consensusMinScore)
		row.Email = finalEmail
		row.Source = finalSource

		addResultOnce(&results, row)

		if row.Email != "" {
			foundCount++
			fmt.Printf("✅ Found: %s => %s\n", contactQuery, row.Email)
		} else {
			fmt.Printf("❌ Keine passende E-Mail gefunden für: %s\n", contactQuery)
		}
	}

	// Ausgabe schreiben
	output := fmt.Sprintf("results_%d.csv", time.Now().Unix())
	if err := Klassische_Pipeline.WriteCSV(output, results); err != nil {
		fmt.Printf("Fehler beim Schreiben der Ergebnisse: %v\n", err)
	} else {
		fmt.Printf("\n💾 Ergebnisse gespeichert in: %s  (Treffer: %d/%d)  ⏱️ Gesamt: %.2fs\n",
			output, foundCount, len(entries), time.Since(startAll).Seconds())
	}
}

// --------------------- Subprozess-Wrapper -----------------------

func scanPDFInSubprocess(ctx context.Context, path, person string) (string, int, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", 0, err
	}
	cmd := exec.CommandContext(ctx, exe, "--scanpdf", path, person)

	// Hartes Heap-Limit im Child via Env (Go 1.19+)
	cmd.Env = append(os.Environ(), "GOMEMLIMIT=200MiB")

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard // PDF-Lib Debug-Noise ignorieren

	if err := cmd.Run(); err != nil {
		// Timeout / Crash → als Fehler zurück; Aufrufer macht einfach weiter
		return "", 0, err
	}

	// Robustes Parsing (letzte OK| Zeile suchen; Debug-Zeilen ignorieren)
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "NONE" || line == "" {
			return "", 0, nil
		}
		if strings.HasPrefix(line, "OK|") {
			parts := strings.Split(line, "|")
			if len(parts) >= 3 {
				sc, _ := strconv.Atoi(parts[2])
				return parts[1], sc, nil
			}
		}
	}
	return "", 0, nil
}

// --------------------------- Query-Helfer -----------------------

func buildQuery(p Klassische_Pipeline.PersonEntry) string {
	name := strings.TrimSpace(p.Name)
	inst := strings.TrimSpace(p.Institution)
	q := strings.TrimSpace(strings.Join([]string{name, inst}, " "))
	return sanitizeQuery(q)
}

// Entfernt E-Mails/URLs/TLD-Schnipsel aus der Such-Query
func sanitizeQuery(q string) string {
	q = strings.TrimSpace(q)
	if q == "" {
		return q
	}
	parts := strings.Fields(q)
	clean := make([]string, 0, len(parts))

	reEmail := regexp.MustCompile(`(?i)^[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}$`)
	reURL := regexp.MustCompile(`(?i)^(https?://|www\.)`)

	skipTLDish := func(s string) bool {
		slow := strings.ToLower(s)
		return strings.Contains(slow, "@") ||
			strings.HasSuffix(slow, ".com") ||
			strings.HasSuffix(slow, ".edu") ||
			strings.HasSuffix(slow, ".org") ||
			strings.HasSuffix(slow, ".net") ||
			strings.Contains(slow, ".co.") ||
			strings.Contains(slow, ".ac.") ||
			strings.Contains(slow, ".uni") ||
			strings.Contains(slow, ".gov")
	}

	for _, p := range parts {
		if reEmail.MatchString(p) {
			continue
		}
		if reURL.MatchString(p) {
			continue
		}
		if skipTLDish(p) {
			continue
		}
		clean = append(clean, p)
	}
	out := strings.Join(clean, " ")
	return strings.Join(strings.Fields(out), " ")
}

// ----------------- Kandidaten / Finale Auswahl ------------------

func shouldEarlyAccept(cands map[string]*candInfo, email string, score int) bool {
	if score >= hardAcceptScore {
		return true
	}
	info := cands[email]
	if info != nil && info.bestScore >= consensusMinScore {
		return true
	}
	return false
}

func registerCandidate(cands map[string]*candInfo, email string, score int, src string) {
	if email == "" {
		return
	}
	info, ok := cands[email]
	if !ok {
		info = &candInfo{bestScore: score, sources: map[string]struct{}{}}
		cands[email] = info
	} else if score > info.bestScore {
		info.bestScore = score
	}
	info.sources[sourceKey(src)] = struct{}{}
}

func pickFinal(cands map[string]*candInfo, minScore int) (email, source string) {
	// 1) Konsens bevorzugen
	for em, info := range cands {
		if info.bestScore >= minScore {
			return em, "(consensus)"
		}
	}
	// 2) sonst besten Kandidaten nehmen
	bestEmail, bestScore := "", -1
	for em, info := range cands {
		if info.bestScore > bestScore {
			bestScore = info.bestScore
			bestEmail = em
		}
	}
	if bestEmail != "" {
		return bestEmail, "(best-overall)"
	}
	return "", ""
}

// Quelle komprimieren (Domain + Pfad). Wenn nur Domain gewünscht: return u.Host
func sourceKey(link string) string {
	u, err := url.Parse(link)
	if err != nil || u.Host == "" {
		return strings.TrimSpace(link)
	}
	return u.Host + u.Path
}

// --------------- Dedupe-Guard fürs Result -----------------------
var seenResults = map[string]struct{}{}

func addResultOnce(results *[]ResultRow, row ResultRow) {
	key := strings.ToLower(strings.TrimSpace(row.Name)) + "|" + strings.ToLower(strings.TrimSpace(row.Email))
	if _, ok := seenResults[key]; ok {
		return
	}
	seenResults[key] = struct{}{}
	*results = append(*results, row)
}

func processLinksCollyEarly(links []string, contactQuery string, candidates map[string]*candInfo) (finalized bool, email, source string) {
	for _, link := range links {
		start := time.Now()
		em, score, err := Klassische_Pipeline.ExtractEmailWithColly(link, contactQuery)
		fmt.Printf("⏱️ [Colly] %s: %.2fs\n", contactQuery, time.Since(start).Seconds())
		if err != nil || em == "" {
			continue
		}
		registerCandidate(candidates, em, score, link)
		if shouldEarlyAccept(candidates, em, score) {
			return true, em, link
		}
	}
	return false, "", ""
}

func processLinksChromedpEarly(links []string, contactQuery string, candidates map[string]*candInfo) (finalized bool, email, source string) {
	for _, link := range links {
		start := time.Now()
		em, score, err := Klassische_Pipeline.ExtractEmailFromURL(link, contactQuery)
		fmt.Printf("⏱️ [Chromedp] %s: %.2fs\n", contactQuery, time.Since(start).Seconds())
		if err != nil || em == "" {
			continue
		}
		registerCandidate(candidates, em, score, link)
		if shouldEarlyAccept(candidates, em, score) {
			return true, em, link
		}
	}
	return false, "", ""
}
