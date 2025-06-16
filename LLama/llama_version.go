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
	"github.com/agnivade/levenshtein"
)

const (
	inputFile  = "list_of_names_and_affiliations.csv"
	outputFile = "results_llama.csv"
	llamaModel = "llama3.3" // oder "llama4:scout", wie in Ollama vorhanden
)

type PersonEntry struct {
	NameAndInstitution string
}

func main() {
	entries, err := ReadCSV(inputFile)
	if err != nil {
		panic(err)
	}

	results := make(map[string]string)

	for i, entry := range entries {
		fmt.Printf("\n➡️ [%d/%d] %s\n", i+1, len(entries), entry.NameAndInstitution)

		links, err := DuckDuckGoSearch(entry.NameAndInstitution)
		if err != nil || len(links) == 0 {
			fmt.Println("⚠️ DuckDuckGo fehlgeschlagen.")
			continue
		}

		for _, link := range links {
			fmt.Println("🔗 Besuche:", link)
			text, err := DownloadPageText(link)
			if err != nil || len(text) < 100 {
				continue
			}

			emails := extractEmails(text)
			bestEmail := findClosestEmail(entry.NameAndInstitution, emails)
			if bestEmail != "" {
				results[entry.NameAndInstitution] = bestEmail
				fmt.Printf("✅ Gefunden: %s\n", bestEmail)
				break
			}

			prompt := BuildLlamaPrompt(entry.NameAndInstitution, text)
			email, err := QueryLlamaREST(prompt)
			if err == nil && isValidEmail(email) {
				results[entry.NameAndInstitution] = email
				fmt.Printf("✅ LLaMA Gefunden: %s\n", email)
				break
			}
		}

		if i%5 == 0 {
			fmt.Printf("🔄 Fortschritt: %d von %d verarbeitet\n", i+1, len(entries))
		}
	}

	err = WriteCSV(outputFile, results)
	if err != nil {
		panic(err)
	}
	fmt.Println("\n✅ Ergebnisse gespeichert in:", outputFile)
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
		entries = append(entries, PersonEntry{
			NameAndInstitution: strings.TrimSpace(record[0]),
		})
	}
	return entries, nil
}

func WriteCSV(path string, results map[string]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()
	_ = w.Write([]string{"Name + Institution", "Email"})
	for k, v := range results {
		_ = w.Write([]string{k, v})
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

func extractEmails(text string) []string {
	re := regexp.MustCompile(`(?i)\b[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}\b`)
	return re.FindAllString(text, -1)
}

func findClosestEmail(name string, emails []string) string {
	bestEmail := ""
	lowestDistance := 1000
	name = strings.ToLower(name)

	for _, email := range emails {
		distance := levenshtein.ComputeDistance(name, strings.ToLower(email))
		if distance < lowestDistance {
			lowestDistance = distance
			bestEmail = email
		}
	}
	return bestEmail
}

func BuildLlamaPrompt(name string, text string) string {
	return fmt.Sprintf(
		"Extrahiere eine E-Mail-Adresse aus folgendem Text, die möglichst gut zu \"%s\" passt:\n\n%s\n\nAntwort nur mit der E-Mail.",
		name, text)
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
		return "Fehler im Llama", err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		Response string `json:"response"`
	}
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		fmt.Println("❌ Llama JSON-Fehler:", err)
		return "", err
	}

	return strings.TrimSpace(result.Response), nil
}

func isValidEmail(email string) bool {
	email = strings.TrimSpace(email)
	if strings.Count(email, "@") != 1 || len(email) > 100 {
		return false
	}
	re := regexp.MustCompile(`(?i)\b[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}\b`)
	return re.MatchString(email)
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
