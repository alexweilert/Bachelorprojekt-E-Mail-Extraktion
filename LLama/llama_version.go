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

type PersonEntry struct {
	NameAndInstitution string
}

func main() {
	inputFile := "list_of_names_and_affiliations.csv"
	outputFile := "results_llama.csv"

	entries, err := ReadLamaCSV(inputFile)
	if err != nil {
		panic(err)
	}

	results := make(map[string]string)

	for i, entry := range entries {
		fmt.Printf("\n➡️ [%d/%d] %s\n", i+1, len(entries), entry.NameAndInstitution)
		links, err := DuckDuckGoLlamaSearch(entry.NameAndInstitution)
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

			prompt := BuildLlamaPrompt(entry.NameAndInstitution, text)
			email, err := QueryLlamaREST(prompt)
			if err == nil && isValidEmail(email) {
				results[entry.NameAndInstitution] = email
				fmt.Printf("✅ Gefunden: %s\n", email)
				break
			}
		}
	}

	err = WriteLlamaCSV(outputFile, results)
	if err != nil {
		panic(err)
	}
	fmt.Println("\n✅ Ergebnisse gespeichert in:", outputFile)
}

func ReadLamaCSV(path string) ([]PersonEntry, error) {
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

func WriteLlamaCSV(path string, results map[string]string) error {
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

func DownloadPageText(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", err
	}

	var builder strings.Builder

	// Text aus sichtbarem DOM
	builder.WriteString(strings.TrimSpace(doc.Text()))

	// Text aus spezifischen E-Mail-ähnlichen Klassen extrahieren
	doc.Find(".email, .email-ta, .SpellE").Each(func(i int, s *goquery.Selection) {
		builder.WriteString("\n" + strings.TrimSpace(s.Text()))
	})

	// Text aus <span>, <p>, <td> mit Email-Keywords
	doc.Find("span, p, td, div").Each(func(i int, s *goquery.Selection) {
		text := s.Text()
		if strings.Contains(strings.ToLower(text), "email") || strings.Contains(text, "@") {
			builder.WriteString("\n" + strings.TrimSpace(text))
		}
	})

	return builder.String(), nil
}

func BuildLlamaPrompt(name string, text string) string {
	return fmt.Sprintf(
		"Extrahiere eine E-Mail-Adresse aus folgendem Text, die möglichst gut zu \"%s\" passt:\n\n%s\n\nAntwort nur mit der E-Mail.",
		name, text)
}

func QueryLlamaREST(prompt string) (string, error) {
	payload := map[string]interface{}{
		"model":  "llama4:scout", // Name exakt wie in Ollama
		"prompt": prompt,
		"stream": false,
	}

	body, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", "http://localhost:11434/api/generate", bytes.NewBuffer(body))
	if err != nil {
		return "Fehler im Llama", err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
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
		return "", err
	}

	return strings.TrimSpace(result.Response), nil
}

func isValidEmail(email string) bool {
	re := regexp.MustCompile(`(?i)\b[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}\b`)
	return re.MatchString(email)
}

func DuckDuckGoLlamaSearch(query string) ([]string, error) {
	time.Sleep(8 * time.Second)

	searchURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)
	client := &http.Client{}

	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")

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
			decoded, err := extractRealDuckDuckGoLlamaURL(href)
			if err == nil {
				urls = append(urls, decoded)
			}
		}
	})

	return urls, nil
}

// Extrahiert aus DuckDuckGo-Umleitungs-URL die echte Ziel-URL
func extractRealDuckDuckGoLlamaURL(href string) (string, error) {
	u, err := url.Parse(href)
	if err != nil {
		return "", err
	}

	q := u.Query()
	realURL := q.Get("uddg")
	realURL, err = url.QueryUnescape(realURL)
	if err != nil {
		return "", err
	}
	return realURL, nil
}
