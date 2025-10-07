package main

import (
	"context"
	"errors"
	"github.com/ledongthuc/pdf"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

// =================== Tuning / Limits ===================

const (
	pdfHTTPTimeout = 35 * time.Second // HTTP timeout for PDF download (langsamer Server!)
	maxPDFBytes    = 8 << 20          // 8 MiB hartes Download-Limit
	pdfTimeBudget  = 20 * time.Second // in-process Zeitbudget (Worker hat zusätzlich 12s)

	maxPagesHardCap       = 120        // nie mehr als so viele Seiten scannen
	initialHeadPages      = 8          // vordere Seiten
	initialTailPages      = 4          // hintere Seiten
	sampleEveryK          = 6          // jede k-te Seite in der Mitte
	perPageTextLimitBytes = 256 * 1024 // Textlimit pro Seite
	highConfidenceCutoff  = 14         // Early-Exit ab Score
	maxCandidatesToScore  = 80         // max. Kandidaten scoren
)

// =================== Regex ===================

var (
	reEmailNormal     = regexp.MustCompile(`(?i)\b[a-z0-9._%+\-]{1,64}@[a-z0-9.\-]{3,}\.[a-z]{2,}\b`)
	reEmailFragmented = regexp.MustCompile(`(?i)([a-z0-9._%+\-]{1,64})\s*\n?\s*@\s*\n?\s*([a-z0-9.\-]{1,200}\.[a-z]{2,})`)
	reEmailSymbolic   = regexp.MustCompile(`(?i)([a-z0-9._+\-]{1,64})\s*(?:\(|\[)?\s*(?:@|at)\s*(?:\)|\])?\s*([a-z0-9.\-\s\[\]\(\)]+)`)
	reNoiseSpaces     = regexp.MustCompile(`\s+`)
	reLocalOK         = regexp.MustCompile(`^[a-z0-9._+\-]{1,64}$`)
	reLabelOK         = regexp.MustCompile(`^[a-z0-9-]+$`)
)

// =================== Download with size limit ===================

func DownloadPDF(u string, filename string) error {
	ctx, cancel := context.WithTimeout(context.Background(), pdfHTTPTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	req.Header.Set("Accept", "application/pdf,application/octet-stream;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9,de;q=0.8")

	client := &http.Client{Timeout: pdfHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Content-Length Vorprüfung gegen Monster-PDFs
	if cl := resp.ContentLength; cl > 0 && cl > maxPDFBytes {
		return errors.New("skip large PDF (content-length)")
	}

	// tolerant: viele Server liefern octet-stream
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if ct != "" && !(strings.HasPrefix(ct, "application/pdf") || strings.HasPrefix(ct, "application/octet-stream")) {
		return errors.New("not a PDF response")
	}

	out, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer out.Close()

	// Hartes Download-Limit
	_, err = io.Copy(out, io.LimitReader(resp.Body, maxPDFBytes))
	return err
}

// =================== PDF email extraction ===================

// Context-fähige Analyse (im Worker aufgerufen)
func ExtractEmailsFromPDFCtx(ctx context.Context, path string, person string) (string, int, error) {
	deadline := time.Now().Add(pdfTimeBudget)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}

	f, r, err := pdf.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	total := r.NumPage()
	if total <= 0 {
		return "", 0, nil
	}
	if total > maxPagesHardCap {
		total = maxPagesHardCap
	}

	pages := planPages(total)
	first, middle, last, org := splitNameAndOrgNoLists(person)

	const (
		perPageTimeBudget  = 800 * time.Millisecond // hartes Limit pro Seite
		docTextBudgetBytes = 1_200_000              // gesamtes Textbudget
		maxTimeoutStrikes  = 2                      // max. Seiten-Timeouts, bevor wir abbrechen
	)

	var (
		bestEmail string
		bestScore = -1
		seen      = make(map[string]struct{})
		scored    = 0
		usedBytes = 0
		strikes   = 0
	)

	consider := func(raw string) bool {
		email := sanitizeEmailTight(raw)
		if email == "" {
			return false
		}
		at := strings.LastIndexByte(email, '@')
		if at <= 0 {
			return false
		}
		domain := strings.ToLower(strings.TrimSpace(email[at+1:]))
		if !validDomain(domain) {
			return false
		}
		if _, ok := seen[email]; ok {
			return false
		}
		seen[email] = struct{}{}

		score := getScoreOrgGeneral(strings.ToLower(email), first, middle, last, org)
		if score > bestScore {
			bestScore = score
			bestEmail = email
		}
		scored++
		return score >= highConfidenceCutoff
	}

	// Seitentext mit hartem Timeout holen
	getPlainTextWithTimeout := func(p pdf.Page, d time.Duration) (string, error) {
		type result struct {
			txt string
			err error
		}
		ch := make(chan result, 1)
		go func() {
			txt, err := p.GetPlainText(nil)
			ch <- result{txt: txt, err: err}
		}()
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(d):
			return "", context.DeadlineExceeded
		case r := <-ch:
			return r.txt, r.err
		}
	}

	for _, i := range pages {
		if time.Now().After(deadline) || scored >= maxCandidatesToScore {
			break
		}
		if time.Until(deadline) < 200*time.Millisecond {
			break
		}

		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		txt, err := getPlainTextWithTimeout(p, perPageTimeBudget)
		if err == context.DeadlineExceeded {
			strikes++
			if strikes > maxTimeoutStrikes {
				break
			}
			continue
		}
		if err != nil || len(txt) == 0 {
			continue
		}

		if len(txt) > perPageTextLimitBytes {
			txt = txt[:perPageTextLimitBytes]
		}

		usedBytes += len(txt)
		if usedBytes > docTextBudgetBytes {
			break
		}

		txt = strings.ReplaceAll(txt, "\u00a0", " ")
		txt = reNoiseSpaces.ReplaceAllString(txt, " ")

		if !pageLikelyHasEmailHint(txt) {
			continue
		}

		// 1) einfache E-Mails
		for _, m := range reEmailNormal.FindAllString(txt, -1) {
			if consider(m) {
				return bestEmail, bestScore, nil
			}
			if time.Now().After(deadline) || scored >= maxCandidatesToScore {
				break
			}
		}
		// 2) fragmentierte
		for _, m := range reEmailFragmented.FindAllStringSubmatch(txt, -1) {
			if len(m) >= 3 {
				if consider(m[1] + "@" + m[2]) {
					return bestEmail, bestScore, nil
				}
			}
			if time.Now().After(deadline) || scored >= maxCandidatesToScore {
				break
			}
		}
		// 3) symbolische
		for _, m := range extractSymbolicEmailsFromText(txt) {
			if consider(m) {
				return bestEmail, bestScore, nil
			}
			if time.Now().After(deadline) || scored >= maxCandidatesToScore {
				break
			}
		}
	}

	if bestEmail == "" {
		return "", 0, nil
	}
	return bestEmail, bestScore, nil
}

// Alte Signatur für evtl. Altaufrufer (ruft ctx-Variante)
func ExtractEmailsFromPDF(path string, person string) (string, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), pdfTimeBudget)
	defer cancel()
	return ExtractEmailsFromPDFCtx(ctx, path, person)
}

// -------------------- Hilfen --------------------

func planPages(total int) []int {
	head := minInt(initialHeadPages, total)
	tail := minInt(initialTailPages, maxInt(0, total-head))
	seen := make(map[int]struct{})
	order := make([]int, 0, minInt(maxPagesHardCap, total))

	// Kopf
	for i := 1; i <= head && len(order) < maxPagesHardCap; i++ {
		order = append(order, i)
		seen[i] = struct{}{}
	}
	// Ende
	for i := total - tail + 1; i <= total && i >= 1 && len(order) < maxPagesHardCap; i++ {
		if _, ok := seen[i]; !ok {
			order = append(order, i)
			seen[i] = struct{}{}
		}
	}
	// Sampling in der Mitte
	start := head + 1
	end := total - tail
	for i := start; i <= end && len(order) < maxPagesHardCap; i += sampleEveryK {
		if _, ok := seen[i]; !ok {
			order = append(order, i)
			seen[i] = struct{}{}
		}
	}
	return order
}

// tight email sanitizer (lokal halten, damit keine Kollisionen entstehen)
func sanitizeEmailTight(raw string) string {
	e := strings.TrimSpace(raw)
	e = strings.Trim(e, "<>\"' .,;:[]()")
	e = strings.ReplaceAll(e, "mailto:", "")
	e = strings.ReplaceAll(e, " ", "")
	if strings.Count(e, "@") != 1 || len(e) > 100 {
		return ""
	}
	if reEmailNormal.MatchString(e) {
		return e
	}
	return ""
}

func pageLikelyHasEmailHint(s string) bool {
	ls := strings.ToLower(s)
	if strings.Contains(ls, "@") {
		return true
	}
	switch {
	case strings.Contains(ls, "email"),
		strings.Contains(ls, "e-mail"),
		strings.Contains(ls, "kontakt"),
		strings.Contains(ls, "contact"),
		strings.Contains(ls, "corresponding author"),
		strings.Contains(ls, "author information"),
		strings.Contains(ls, "impressum"):
		return true
	}
	return false
}

// symbolische E-Mails zusammensetzen
func extractSymbolicEmailsFromText(text string) []string {
	matches := reEmailSymbolic.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		local := strings.ToLower(strings.TrimSpace(m[1]))
		if !reLocalOK.MatchString(local) {
			continue
		}
		d := strings.ToLower(m[2])
		// "dot"-Schreibweisen normalisieren
		for _, r := range []string{" (dot) ", " dot ", "[dot]", "(dot)", " DOT ", " Dot ", " dot", "dot "} {
			d = strings.ReplaceAll(d, r, ".")
		}
		d = strings.ReplaceAll(d, " ", "")
		d = strings.ReplaceAll(d, "[.]", ".")
		d = strings.ReplaceAll(d, "(.)", ".")
		d = regexp.MustCompile(`\.{2,}`).ReplaceAllString(d, ".")
		d = strings.Trim(d, ".")
		if !validDomain(d) {
			continue
		}
		out = append(out, local+"@"+d)
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
