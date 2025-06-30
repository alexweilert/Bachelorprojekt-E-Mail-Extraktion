package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
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

const (
	inputFile  = "list_of_names_and_affiliations.csv"
	outputFile = "results_llama.csv"
	llamaModel = "llama2:7b"
)

type PersonEntry struct {
	NameAndInstitution string
}

type ResultRow struct {
	Name   string
	Email  string
	Time   string
	Source string
}

func main() {
	entries, err := ReadCSV(inputFile)
	if err != nil {
		panic(err)
	}

	var results []ResultRow
	totalStart := time.Now()
	foundCount := 0

	for i, entry := range entries {
		fmt.Printf("\n➡️ [%d/%d] %s\n", i+1, len(entries), entry.NameAndInstitution)
		start := time.Now()
		row := ResultRow{Name: entry.NameAndInstitution, Email: "", Source: ""}

		links, err := DuckDuckGoSearch(entry.NameAndInstitution)
		if err != nil || len(links) == 0 {
			fmt.Println("⚠️ DuckDuckGo fehlgeschlagen.")
			row.Time = fmt.Sprintf("%.2fs", time.Since(start).Seconds())
			results = append(results, row)
			continue
		}

		for _, link := range links {
			fmt.Println("🔗 Besuche:", link)
			text, err := DownloadPageText(link)
			if err != nil || len(text) < 100 {
				continue
			}

			prompt := BuildLlamaPrompt(entry.NameAndInstitution, text)
			email, err := QueryLlamaREST(prompt)
			if err == nil {
				email = sanitizeEmail(email)
				if email != "" {
					row.Email = email
					row.Source = link
					foundCount++
					fmt.Printf("✅ LLaMA Gefunden: %s\n", email)
					break
				}
			}
		}
		row.Time = fmt.Sprintf("%.2fs", time.Since(start).Seconds())
		results = append(results, row)

		if i%5 == 0 {
			fmt.Printf("🔄 Fortschritt: %d von %d verarbeitet\n", i+1, len(entries))
		}
	}
	totalDuration := time.Since(totalStart)
	fmt.Printf("\n✅ %d von %d E-Mails gefunden\n", foundCount, len(entries))
	fmt.Printf("⏱️ Gesamtdauer: %.2fs\n", totalDuration.Seconds())

	err = WriteCSV(outputFile, results)
	if err != nil {
		panic(err)
	}
	fmt.Println("\n📄 Ergebnisse gespeichert in:", outputFile)
}

func ReadCSV(path string) ([]PersonEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	var entries []PersonEntry
	for {
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil || len(record) == 0 {
			continue
		}
		entries = append(entries, PersonEntry{strings.TrimSpace(record[0])})
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
	_ = w.Write([]string{"Name + Institution", "Email", "Zeit", "Gefunden auf"})
	for _, row := range results {
		_ = w.Write([]string{row.Name, row.Email, row.Time, row.Source})
	}
	return nil
}

func DownloadPageText(pageURL string) (string, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(pageURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", err
	}

	var builder strings.Builder
	builder.WriteString(strings.TrimSpace(doc.Text()))

	doc.Find(".email, .email-ta, .SpellE").Each(func(i int, s *goquery.Selection) {
		builder.WriteString("\n" + strings.TrimSpace(s.Text()))
	})
	doc.Find("span, p, td, div").Each(func(i int, s *goquery.Selection) {
		text := s.Text()
		if strings.Contains(strings.ToLower(text), "email") || strings.Contains(text, "@") {
			builder.WriteString("\n" + strings.TrimSpace(text))
		}
	})

	return builder.String(), nil
}

func BuildLlamaPrompt(name string, text string) string {
	return fmt.Sprintf(`Die folgende Person heißt "%s". Der folgende Text enthält möglicherweise Kontaktinformationen.

Bitte finde genau **eine einzige** E-Mail-Adresse, die **am besten** zu dieser Person passt.
Beachte:
- Nutze auch Initialen oder Namensfragmente (z. B. j.smith für John Smith).
- Bevorzuge E-Mails mit Bezug zu Institutionen (z. B. Universitäten).
- Gib **nur die E-Mail-Adresse** zurück – **ohne Anführungszeichen, Klammern oder Sonderzeichen**.
- Wenn keine passende E-Mail gefunden wurde, gib einfach "" zurück.

Text:
---
%s
---

Antwort nur mit der E-Mail-Adresse:`, name, text)
}

func QueryLlamaREST(prompt string) (string, error) {
	payload := map[string]interface{}{
		"model":  llamaModel,
		"prompt": prompt,
		"stream": false,
	}

	body, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", "http://localhost:11434/api/generate", bytes.NewBuffer(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("❌ LLaMA ist nicht erreichbar.")
		return "", err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		fmt.Println("❌ LLaMA JSON-Fehler:", err)
		return "", err
	}

	if errMsg, ok := result["error"].(string); ok && errMsg != "" {
		fmt.Println("❌ LLaMA Fehler:", errMsg)
		return "", fmt.Errorf("llama error: %s", errMsg)
	}

	if response, ok := result["response"].(string); ok && strings.TrimSpace(response) != "" {
		emailLines := strings.Split(strings.TrimSpace(response), "\n")
		for _, line := range emailLines {
			clean := sanitizeEmail(line)
			if clean != "" {
				return clean, nil
			}
		}
	}

	return "", fmt.Errorf("keine gültige Antwort von LLaMA erhalten")
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

func DuckDuckGoSearch(query string) ([]string, error) {
	time.Sleep(3 * time.Second)

	searchURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)
	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")

	client := &http.Client{Timeout: 15 * time.Second}
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
	doc.Find(".result__a").Each(func(i int, s *goquery.Selection) {
		href, exists := s.Attr("href")
		if exists {
			u, err := url.Parse(href)
			if err == nil {
				uddg := u.Query().Get("uddg")
				decoded, err := url.QueryUnescape(uddg)
				if err == nil && strings.HasPrefix(decoded, "http") {
					urls = append(urls, decoded)
				}
			}
		}
	})

	return urls, nil
}
