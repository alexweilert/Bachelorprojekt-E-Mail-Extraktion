package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// -------- Options & Defaults --------

type ddgOptions struct {
	Limit         int           // maximale Anzahl an Ergebnis-URLs
	Timeout       time.Duration // gesamter Timeout je Suche
	ReqTimeout    time.Duration // Timeout pro HTTP-Request
	VerifyLinks   bool          // HEAD-Check der gefundenen URLs
	Workers       int           // parallele Verifizierungs-Worker
	PDFOnly       bool          // nur .pdf-Links zurückgeben
	MinDelay      time.Duration // min. Delay zwischen Seitenabfragen
	MaxDelay      time.Duration // max. Delay zwischen Seitenabfragen
	UserAgent     string        // User-Agent für Requests
	MaxPages      int           // Sicherheitslimit für Seiten
	PageSizeGuess int           // ungefähre Treffer/Seite (für Offset)
}

func defaultDDGOptions() ddgOptions {
	return ddgOptions{
		Limit:         25,
		Timeout:       25 * time.Second,
		ReqTimeout:    10 * time.Second,
		VerifyLinks:   true,
		Workers:       6,
		PDFOnly:       false,
		MinDelay:      300 * time.Millisecond,
		MaxDelay:      1200 * time.Millisecond,
		UserAgent:     "Mozilla/5.0 (Windows NT 10.0; Win64; x64)",
		MaxPages:      6,  // bis ~300 Ergebnisse (je nach DDG)
		PageSizeGuess: 50, // DDG HTML liefert ca. 30–50/Seite
	}
}

// -------- Öffentliche Wrapper (API bleibt stabil) --------

// Wie bisher: allgemeine Websuche → URLs
func DuckDuckGoSearch(query string) ([]string, error) {
	opts := defaultDDGOptions()
	opts.PDFOnly = false
	return duckDuckGoSearch(query, opts)
}

// DuckDuckGoPDFSearch baut "höfliche" Defaults und ruft deine bestehende duckDuckGoSearch(query, opts)
func DuckDuckGoPDFSearch(query string) ([]string, error) {
	opts := defaultDDGOptions()
	opts.PDFOnly = true
	opts.Limit = 6 // passend zu main.maxLinksPDF
	opts.MaxPages = 2
	opts.VerifyLinks = false // keine parallelen HEAD-Checks
	opts.Workers = 1
	opts.MinDelay = 1500 * time.Millisecond
	opts.MaxDelay = 3500 * time.Millisecond
	opts.Timeout = 60 * time.Second    // Gesamtbudget
	opts.ReqTimeout = 12 * time.Second // pro Request
	return duckDuckGoSearch(query, opts)
}

/*
// Wie bisher: nur PDF-Links → URLs
func DuckDuckGoPDFSearch(query string) ([]string, error) {
	opts := defaultDDGOptions()
	opts.PDFOnly = true
	return duckDuckGoSearch(query, opts)
}
*/

// -------- Kernsuche --------

func duckDuckGoSearch(query string, opts ddgOptions) ([]string, error) {
	if strings.TrimSpace(query) == "" {
		return nil, errors.New("leere Suchanfrage")
	}

	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	defer cancel()

	client := &http.Client{Timeout: opts.ReqTimeout}
	var (
		results   = make([]string, 0, opts.Limit)
		seen      = make(map[string]struct{}, opts.Limit*2)
		pageIndex = 0
	)

	// Pseudo-Zufall für Jitter
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	for pageIndex < opts.MaxPages && len(results) < opts.Limit {
		// DuckDuckGo HTML-Endpoint + Offset ("s")
		searchURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)
		if pageIndex > 0 {
			offset := pageIndex * opts.PageSizeGuess
			searchURL += "&s=" + url.QueryEscape(fmt.Sprintf("%d", offset))
		}

		req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", opts.UserAgent)

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("ddg request failed: %w", err)
		}
		doc, err := goquery.NewDocumentFromReader(resp.Body)
		if err != nil {
			resp.Body.Close()
			return nil, err
		}
		resp.Body.Close()

		// Links extrahieren
		pageURLs := make([]string, 0, 50)
		doc.Find(".result__a").Each(func(i int, s *goquery.Selection) {
			href, ok := s.Attr("href")
			if !ok || href == "" {
				return
			}
			decoded, err := extractRealDuckDuckGoURL(href)
			if err != nil || decoded == "" {
				return
			}
			if opts.PDFOnly && !strings.HasSuffix(strings.ToLower(decoded), ".pdf") {
				return
			}
			// Normieren: Whitespace raus, http→https wenn möglich (keine strikte Umwandlung)
			u := strings.TrimSpace(decoded)
			if _, exists := seen[u]; exists {
				return
			}
			seen[u] = struct{}{}
			pageURLs = append(pageURLs, u)
		})

		// Optional: Link-Verifikation (HEAD) parallel
		if opts.VerifyLinks && len(pageURLs) > 0 {
			pageURLs = headVerifyBatch(ctx, client, pageURLs, opts.Workers)
		}

		// Ergebnisse anhängen bis Limit
		for _, u := range pageURLs {
			if len(results) >= opts.Limit {
				break
			}
			results = append(results, u)
		}

		// Stop-Kriterien
		if len(pageURLs) == 0 {
			// keine weiteren Ergebnisse gefunden → Abbruch
			break
		}
		if len(results) >= opts.Limit {
			break
		}

		// Höflichkeitspause mit Jitter
		sleep := opts.MinDelay
		if opts.MaxDelay > opts.MinDelay {
			delta := opts.MaxDelay - opts.MinDelay
			sleep += time.Duration(rng.Int63n(int64(delta)))
		}
		select {
		case <-time.After(sleep):
		case <-ctx.Done():
			break
		}
		pageIndex++
	}

	return results, nil
}

// Extrahiert aus DuckDuckGo-Umleitungs-URL die echte Ziel-URL
func extractRealDuckDuckGoURL(href string) (string, error) {
	u, err := url.Parse(href)
	if err != nil {
		return "", err
	}
	q := u.Query()
	realURL := q.Get("uddg")
	if realURL == "" {
		// Fallback: DDG liefert manchmal direkte Links
		return href, nil
	}
	realURL, err = url.QueryUnescape(realURL)
	if err != nil {
		return "", err
	}
	return realURL, nil
}

// sichere, cancel-freundliche Link-Verifikation
func headVerifyBatch(ctx context.Context, client *http.Client, urls []string, workers int) []string {
	if workers < 1 {
		workers = 1
	}

	type job struct{ u string }
	type res struct {
		u  string
		ok bool
	}

	jobs := make(chan job)
	out := make(chan res)

	var wg sync.WaitGroup
	worker := func() {
		defer wg.Done()
		for j := range jobs {
			ok := false

			// 1) HEAD versuchen
			req, err := http.NewRequestWithContext(ctx, "HEAD", j.u, nil)
			if err == nil {
				req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
				if resp, err2 := client.Do(req); err2 == nil {
					if resp.Body != nil {
						resp.Body.Close()
					}
					if resp.StatusCode < 400 {
						ok = true
					} else if resp.StatusCode == http.StatusMethodNotAllowed {
						// 2) Fallback: GET (kurz), wenn HEAD nicht erlaubt ist
						reqGet, err3 := http.NewRequestWithContext(ctx, "GET", j.u, nil)
						if err3 == nil {
							reqGet.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
							// kleine Deadline, um nicht zu hängen
							getClient := *client
							getClient.Timeout = 5 * time.Second
							if resp2, err4 := getClient.Do(reqGet); err4 == nil {
								if resp2.Body != nil {
									resp2.Body.Close()
								}
								if resp2.StatusCode < 400 {
									ok = true
								}
							}
						}
					}
				}
			}

			select {
			case out <- res{u: j.u, ok: ok}:
			case <-ctx.Done():
				return
			}
		}
	}

	// Worker starten
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go worker()
	}

	// Jobs verteilen
	go func() {
		for _, u := range urls {
			select {
			case jobs <- job{u: u}:
			case <-ctx.Done():
				close(jobs)
				return
			}
		}
		close(jobs)
	}()

	// out schließen, nachdem alle Worker fertig sind
	go func() {
		wg.Wait()
		close(out)
	}()

	// Ergebnisse einsammeln
	verified := make([]string, 0, len(urls))
	for r := range out {
		if r.ok {
			verified = append(verified, r.u)
		}
	}
	return verified
}
