package main

import (
	"errors"
	"github.com/ledongthuc/pdf"
	"io"
	"net/http"
	"os"
	"regexp"
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
			rawMatches := emailPattern.FindAllString(content, -1)
			for _, raw := range rawMatches {
				clean := sanitizeEmail(raw)
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
		return result.email, result.score, result.err
	case <-time.After(30 * time.Second):
		return "", 0, errors.New("timeout: ExtractEmailsFromPDF took too long")
	}
}
