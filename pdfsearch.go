package main

import (
	"errors"
	"fmt"
	"github.com/ledongthuc/pdf"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

func DownloadPDF(url string, filename string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	out, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func ExtractEmailsFromPDF(filepath string, name string) (string, int, error) {
	start := time.Now()
	resultChan := make(chan struct {
		email string
		score int
		err   error
	}, 1)

	go func() {
		f, r, err := pdf.Open(filepath)
		if err != nil {
			resultChan <- struct {
				email string
				score int
				err   error
			}{"", 0, err}
			return
		}
		defer f.Close()

		emailPattern := regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}`)
		fragmentedPattern := regexp.MustCompile(`(?i)([a-z0-9._%+\-]+)\s*\n?\s*@\s*\n?\s*([a-z0-9.\-]+\.[a-z]{2,})`)
		symbolicPattern := regexp.MustCompile(`(?i)([a-z0-9._%+\-]+)\s*(\[?at\]?|\(at\)|\s+at\s+)\s*([a-z0-9.\-]+\s*(dot|\.|\(dot\)|\[dot\])\s*[a-z]{2,})`)

		allEmails := make(map[string]bool)
		totalPage := r.NumPage()
		for i := 1; i <= totalPage; i++ {
			page := r.Page(i)
			if page.V.IsNull() {
				continue
			}
			content, err := page.GetPlainText(nil)
			if err != nil {
				continue
			}

			content = strings.ReplaceAll(content, "\u00a0", " ")
			content = strings.ReplaceAll(content, "\n", " ")
			content = strings.Join(strings.Fields(content), " ")

			rawMatches := emailPattern.FindAllString(content, -1)
			for _, raw := range rawMatches {
				clean := sanitizeEmail(raw)
				if clean != "" {
					allEmails[clean] = true
				}
			}

			// Fragmentierte E-Mails zusammensetzen
			fragments := fragmentedPattern.FindAllStringSubmatch(content, -1)
			for _, frag := range fragments {
				composed := frag[1] + "@" + frag[2]
				clean := sanitizeEmail(composed)
				if clean != "" {
					allEmails[clean] = true
				}
			}

			// Symbolische E-Mails ersetzen
			symbolics := symbolicPattern.FindAllStringSubmatch(content, -1)
			for _, sym := range symbolics {
				domain := strings.ReplaceAll(sym[3], " dot ", ".")
				domain = strings.ReplaceAll(domain, " (dot) ", ".")
				domain = strings.ReplaceAll(domain, "[dot]", ".")
				domain = strings.ReplaceAll(domain, " ", "")
				composed := sym[1] + "@" + domain
				clean := sanitizePDFMail(composed)
				if clean != "" {
					allEmails[clean] = true
				}
			}
		}

		var bestEmail string
		bestScore := -1
		firstName, middleName, lastName := extractNameParts(name)

		for email := range allEmails {
			score := getScore(email, firstName, middleName, lastName)
			if score > bestScore {
				bestEmail = email
				bestScore = score
			}
		}

		if bestEmail == "" {
			resultChan <- struct {
				email string
				score int
				err   error
			}{"", 0, nil}
			return
		}
		resultChan <- struct {
			email string
			score int
			err   error
		}{bestEmail, bestScore, nil}
	}()

	select {
	case result := <-resultChan:
		elapsed := time.Since(start)
		fmt.Printf("⏱️ [PDF] %s: %.2fs\n", name, elapsed.Seconds())
		return result.email, result.score, result.err
	case <-time.After(30 * time.Second):
		return "", 0, errors.New("timeout: ExtractEmailsFromPDF took too long")
	}
}

func sanitizePDFMail(raw string) string {
	email := strings.TrimSpace(raw)
	email = strings.ReplaceAll(email, "mailto:", "")
	email = strings.Trim(email, "<> \t\n\r\"',.;:[]{}")
	// Nur die gültige E-Mail extrahieren
	re := regexp.MustCompile(`(?i)\b[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}\b`)
	match := re.FindString(email)
	if match != "" {
		return match
	}
	return ""
}
