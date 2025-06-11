package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

func DuckDuckGoSearch(query string) ([]string, error) {
	// time.Sleep(5 * time.Second)

	searchURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

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
			decoded, err := extractRealDuckDuckGoURL(href)
			if err == nil {
				urls = append(urls, decoded)
			}
		}
	})

	return urls, nil
}

// Extrahiert aus DuckDuckGo-Umleitungs-URL die echte Ziel-URL
func extractRealDuckDuckGoURL(href string) (string, error) {
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
	//fmt.Println(" ➤ Extrahierter echter Link:", realURL)
	return realURL, nil
}

func BingSearch(query string) ([]string, error) {
	//time.Sleep(3 * time.Second)
	searchURL := "https://www.bing.com/search?q=" + url.QueryEscape(query)
	fmt.Printf("🌐 Bing-Such-URL: %s\n", searchURL)

	req, _ := http.NewRequest("GET", searchURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/122 Safari/537.36")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	var urls []string
	doc.Find("a").Each(func(i int, s *goquery.Selection) {
		href, exists := s.Attr("href")
		if exists && isValidBingLink(href) {
			urls = append(urls, href)
		}
	})

	if len(urls) == 0 {
		fmt.Println("⚠️ Bing hat keine gültigen Links geliefert.")
	} else {
		for _, url := range urls {
			fmt.Println(" ➤ [Bing] Gefilterter Link:", url)
		}
	}

	return urls, nil
}

func isValidBingLink(href string) bool {
	if !strings.HasPrefix(href, "http") {
		return false
	}
	if strings.Contains(href, "microsoft.com") ||
		strings.Contains(href, "bing.com") ||
		strings.Contains(href, "go.microsoft.com") ||
		strings.Contains(href, "/search") ||
		strings.Contains(href, "webcache") {
		return false
	}
	return true
	// Bevorzugte Domains
	//whitelist := []string{".edu", ".ac.", ".org", ".de", ".fr", ".nl", ".ch"}
	//for _, tld := range whitelist {
	//	if strings.Contains(href, tld) {
	//		return true
	//	}
	//}

	// Optional: trotzdem erlauben, wenn z. B. `linkedin.com/in/` etc.
	//if strings.Contains(href, "linkedin.com/in/") {
	//	return true
	//}

	//return false
}

func DuckDuckGoPDFSearch(query string) ([]string, error) {
	time.Sleep(5 * time.Second)

	// Suche nach PDF-Dateien mit filetype-Filter
	fullQuery := query + " filetype:pdf"
	searchURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(fullQuery)

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

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
			decoded, err := extractRealDuckDuckGoURL(href)
			if err == nil && strings.HasSuffix(strings.ToLower(decoded), ".pdf") {
				urls = append(urls, decoded)
			}
		}
	})

	if len(urls) == 0 {
		fmt.Println("⚠️ DuckDuckGo (PDF) hat keine PDFs geliefert.")
	} else {
		for _, link := range urls {
			fmt.Println("📄 [DuckDuckGo PDF] Link:", link)
		}
	}

	return urls, nil
}
