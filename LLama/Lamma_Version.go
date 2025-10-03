package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// ==============================================
// Konfiguration
// ==============================================
const (
	inputFile  = "list_of_names_and_affiliations.csv" // 2 Spalten: Name | Institution
	outputFile = "results_llama.csv"                  // 3 Spalten: Name | Institution | Email

	// Stelle sicher, dass Ollama läuft und das Modell lokal verfügbar ist:
	//   ollama pull llama3:8b
	llamaModel = "llama2:7b"

	maxLinksPerPerson   = 8
	maxContextChars     = 2200
	httpRequestTimeout  = 20 * time.Second
	searchPolitenessGap = 3 * time.Second
)

// ==============================================
// Datentypen
// ==============================================

type PersonEntry struct {
	Name        string
	Institution string
}

type ResultRow struct {
	Name        string
	Institution string
	Email       string
}

// LLAMA-JSON
type llamaDecision struct {
	Email      string `json:"email"`
	Confidence int    `json:"confidence"`
	Reason     string `json:"reason"`
}

// ==============================================
// main
// ==============================================

func main() {
	entries, err := ReadCSV(inputFile)
	if err != nil {
		panic(err)
	}

	var results []ResultRow
	totalStart := time.Now()
	foundCount := 0

	for i, e := range entries {
		display := strings.Trim(strings.TrimSpace(e.Name+", "+e.Institution), ", ")
		fmt.Printf("\n➡️  [%d/%d] %s\n", i+1, len(entries), display)
		start := time.Now()

		row := ResultRow{Name: e.Name, Institution: e.Institution}

		// 1) Suche nach potenziellen Profil-/Kontaktseiten
		queries := uniqueStrings([]string{
			strings.TrimSpace(e.Name + " " + e.Institution + " email"),
			strings.TrimSpace(e.Name + " " + e.Institution + " contact"),
			strings.TrimSpace(e.Name + " " + e.Institution + " faculty email"),
			strings.TrimSpace(e.Name + " email"),
			strings.TrimSpace(e.Name + " contact"),
		})

		seen := map[string]bool{}
		var links []string
		for _, q := range queries {
			if q == "" {
				continue
			}
			us, err := DuckDuckGoSearch(q)
			if err != nil {
				fmt.Println("⚠️ DuckDuckGo fehlgeschlagen:", err)
				continue
			}
			for _, u := range us {
				if !seen[u] {
					links = append(links, u)
					seen[u] = true
				}
				if len(links) >= maxLinksPerPerson {
					break
				}
			}
			time.Sleep(searchPolitenessGap)
			if len(links) >= maxLinksPerPerson {
				break
			}
		}

		if len(links) == 0 {
			fmt.Println("⚠️ Keine Links gefunden.")
			results = append(results, row)
			continue
		}

		// 2) Seiten nacheinander laden und LLAMA entscheiden lassen
		found := false
		for _, link := range links {
			fmt.Println("🔗 Besuche:", link)
			text, emails, _, err := FetchPageAndCollect(link)
			if err != nil || len(text) < 100 {
				continue
			}

			// Kontext kürzen: wenn E-Mails im Text vorkommen, nimm ein Fenster darum; sonst Kopfstück
			ctx := trimContextAroundEmails(text, emails, maxContextChars)
			if ctx == "" {
				ctx = truncate(text, maxContextChars)
			}

			email, reason := analyzePageWithLlama(e.Name, e.Institution, link, ctx)
			email = sanitizeEmail(email)
			if email != "" {
				row.Email = email
				found = true
				foundCount++
				fmt.Printf("✅ LLaMA gefunden: %s\n", email)
				fmt.Println("   Quelle:", link) // Webseite weiterhin in der Konsole ausgeben
				break
			} else {
				fmt.Println("ℹ️  Seite ohne valide Adresse laut LLaMA:", reason)
			}
		}

		if !found {
			fmt.Println("❌ Keine passende E-Mail auf den geprüften Seiten gefunden.")
		}

		fmt.Printf("⏱️ Dauer: %.2fs\n", time.Since(start).Seconds())
		results = append(results, row)
	}

	fmt.Printf("\n✅ %d von %d E-Mails gefunden\n", foundCount, len(entries))
	fmt.Printf("⏱️ Gesamtdauer: %.2fs\n", time.Since(totalStart).Seconds())

	if err := WriteCSV(outputFile, results); err != nil {
		panic(err)
	}
	fmt.Println("\n📄 Ergebnisse gespeichert in:", outputFile)
}

// ==============================================
// CSV I/O (2 Spalten: Name | Institution)
// ==============================================

func ReadCSV(path string) ([]PersonEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	var entries []PersonEntry
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil || len(rec) == 0 {
			continue
		}

		name := strings.TrimSpace(rec[0])
		inst := ""
		if len(rec) >= 2 {
			inst = strings.TrimSpace(rec[1])
		}
		if name == "" && inst == "" {
			continue
		}
		entries = append(entries, PersonEntry{Name: name, Institution: inst})
	}
	return entries, nil
}

func WriteCSV(path string, results []ResultRow) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()
	// _ = w.Write([]string{"Name", "Institution", "Email"}) // nur 3 Spalten
	for _, row := range results {
		_ = w.Write([]string{row.Name, row.Institution, row.Email})
	}
	return nil
}

// ==============================================
// Suche & Scraping
// ==============================================

func DuckDuckGoSearch(query string) ([]string, error) {
	time.Sleep(searchPolitenessGap)
	searchURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)
	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")

	client := &http.Client{Timeout: httpRequestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	var urls []string
	doc.Find(".result__a").Each(func(_ int, s *goquery.Selection) {
		if href, ok := s.Attr("href"); ok {
			u, err := url.Parse(href)
			if err == nil {
				uddg := u.Query().Get("uddg")
				if decoded, err := url.QueryUnescape(uddg); err == nil && strings.HasPrefix(decoded, "http") {
					urls = append(urls, decoded)
				}
			}
		}
	})
	return urls, nil
}

// Lädt Seite, holt Text & extrahiert offensichtliche E-Mail-Kandidaten (nur fürs Kontextfenster)
func FetchPageAndCollect(pageURL string) (string, []string, string, error) {
	client := &http.Client{Timeout: httpRequestTimeout}
	resp, err := client.Get(pageURL)
	if err != nil {
		return "", nil, "", err
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", nil, "", err
	}

	var b strings.Builder
	b.WriteString(strings.TrimSpace(doc.Text()))

	// Zusätzliche Felder, die oft E-Mails enthalten
	doc.Find(".email, .email-ta, .SpellE").Each(func(_ int, s *goquery.Selection) {
		b.WriteString("\n" + strings.TrimSpace(s.Text()))
	})
	doc.Find("span, p, td, div, a").Each(func(_ int, s *goquery.Selection) {
		text := s.Text()
		if strings.Contains(strings.ToLower(text), "email") || strings.Contains(text, "@") {
			b.WriteString("\n" + strings.TrimSpace(text))
		}
		if href, ok := s.Attr("href"); ok && strings.HasPrefix(strings.ToLower(href), "mailto:") {
			b.WriteString("\n" + href)
		}
	})

	pageText := b.String()
	emails := extractEmailsRobust(pageText)
	return pageText, emails, extractDomain(pageURL), nil
}

// ==============================================
// LLAMA: Seitenanalyse
// ==============================================

func analyzePageWithLlama(name, institution, pageURL, context string) (string, string) {
	prompt := BuildLlamaPromptPage(name, institution, pageURL, context)
	dec, err := QueryLlamaJSON(prompt)
	if err != nil {
		return "", err.Error()
	}
	return dec.Email, dec.Reason
}

func BuildLlamaPromptPage(name, institution, pageURL, context string) string {
	return fmt.Sprintf(`Du bist ein präziser E-Mail-Erkennungs-Assistent.
Person: "%s".
Institution: "%s".
Quelle: %s

Lies den folgenden Seitentext (ggf. gekürzt). Prüfe NUR, ob dort explizit eine E-Mail-Adresse dieser Person genannt ist. Halluziniere NICHT.
---
%s
---

Antworte ausschließlich als JSON-Objekt mit Feldern {"email":string,"confidence":0-100,"reason":string}.
Regeln:
- Gib eine E-Mail NUR zurück, wenn sie im Text wirklich vorkommt und mit Person/Institution plausibel verknüpft ist (z. B. Name/Initialen, passende Domain).
- Wenn keine passende E-Mail vorhanden ist, antworte mit {"email":"","confidence":0,"reason":"no match"}.`,
		name, institution, pageURL, truncate(context, maxContextChars))
}

func QueryLlamaJSON(prompt string) (llamaDecision, error) {
	payload := map[string]interface{}{
		"model":       llamaModel,
		"prompt":      prompt,
		"stream":      false,
		"temperature": 0.2,
		"format":      "json",
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", "http://localhost:11434/api/generate", bytes.NewBuffer(body))
	if err != nil {
		return llamaDecision{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: httpRequestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return llamaDecision{}, err
	}
	defer resp.Body.Close()

	var result struct {
		Response string `json:"response"`
		Error    string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return llamaDecision{}, err
	}
	if result.Error != "" {
		return llamaDecision{}, errors.New(result.Error)
	}

	respText := strings.TrimSpace(result.Response)
	respText = strings.Trim(respText, "` ")

	var dec llamaDecision
	if err := json.Unmarshal([]byte(respText), &dec); err != nil {
		// Notfall: versuche reine E-Mail-Linie zu extrahieren
		if mail := sanitizeEmail(respText); mail != "" {
			return llamaDecision{Email: mail, Confidence: 50, Reason: "fallback: simple parse"}, nil
		}
		return llamaDecision{}, err
	}
	return dec, nil
}

// ==============================================
// Hilfsfunktionen
// ==============================================

func extractDomain(u string) string {
	pu, err := url.Parse(u)
	if err != nil {
		return ""
	}
	host := pu.Host
	host = strings.TrimPrefix(host, "www.")
	return host
}

func extractEmailsRobust(text string) []string {
	// 1) Normale E-Mails
	re := regexp.MustCompile(`(?i)\b[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}\b`)
	set := map[string]bool{}
	for _, m := range re.FindAllString(text, -1) {
		if e := sanitizeEmail(m); e != "" {
			set[e] = true
		}
	}
	// 2) Fragmentierte Formen: name [at] uni [dot] de
	obf := regexp.MustCompile(`(?i)([a-z0-9._%+\-]+)\s*(\[?at\]?|\(at\)|\sat\s|\s@\s)\s*([a-z0-9.\-]+)\s*(\[?dot\]?|\(dot\)|\sdot\s|\.)\s*([a-z]{2,})`)
	for _, m := range obf.FindAllStringSubmatch(text, -1) {
		cand := m[1] + "@" + strings.ReplaceAll(m[3], " ", "") + "." + m[5]
		if e := sanitizeEmail(cand); e != "" {
			set[e] = true
		}
	}
	res := make([]string, 0, len(set))
	for e := range set {
		res = append(res, e)
	}
	return res
}

func sanitizeEmail(raw string) string {
	email := strings.TrimSpace(raw)
	email = strings.Trim(email, "<>\"' .,;:[]()")
	email = strings.ReplaceAll(email, "mailto:", "")
	email = strings.ReplaceAll(email, " ", "")
	if strings.Count(email, "@") != 1 || len(email) > 100 {
		return ""
	}
	re := regexp.MustCompile(`(?i)\b[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}\b`)
	if re.MatchString(email) {
		return email
	}
	return ""
}

func trimContextAroundEmails(text string, emails []string, limit int) string {
	if text == "" {
		return ""
	}
	if len(text) <= limit {
		return text
	}
	if len(emails) == 0 {
		return text[:limit]
	}
	first := emails[0]
	idx := strings.Index(text, first)
	if idx == -1 {
		return text[:limit]
	}
	start := idx - limit/2
	if start < 0 {
		start = 0
	}
	end := start + limit
	if end > len(text) {
		end = len(text)
	}
	return text[start:end]
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
