package main

import (
	"io"
	"net/http"
	"os"
	"regexp"

	"github.com/ledongthuc/pdf"
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

func ExtractEmailsFromPDF(filepath string) ([]string, error) {
	f, r, err := pdf.Open(filepath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var emails []string
	emailPattern := regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}`)

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
		found := emailPattern.FindAllString(content, -1)
		emails = append(emails, found...)
	}
	return emails, nil
}
