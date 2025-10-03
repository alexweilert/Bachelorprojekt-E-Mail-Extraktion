package Old_Data

import (
	"github.com/agnivade/levenshtein"
	"strings"
)

func getScore(email, firstName, middleName, lastName string) int {
	email = strings.ToLower(email)
	score := 0

	nameParts := []string{firstName, middleName, lastName}

	// 1. Direkte Substring-Matches (4 Punkte)
	for _, part := range nameParts {
		if part != "" && strings.Contains(email, part) {
			score += 4
		}
	}

	// 2. Initialen + Kombinationen (3 Punkte)
	initialCombos := generateInitialCombos(firstName, middleName, lastName)
	for _, combo := range initialCombos {
		if strings.Contains(email, combo) {
			score += 3
		}
	}

	// 3. Kürzere Substrings (3 Punkte)
	if len(lastName) >= 4 && strings.Contains(email, lastName[:4]) {
		score += 3
	}
	if len(firstName) >= 3 && strings.Contains(email, firstName[:3]) {
		score += 3
	}

	// 4. Levenshtein-basierte Ähnlichkeit (max 5 Punkte)
	fullName := firstName + middleName + lastName
	if fullName != "" {
		localPart := strings.Split(email, "@")[0]
		distance := levenshtein.ComputeDistance(localPart, fullName)
		normalized := float64(distance) / float64(len(fullName)+1)
		switch {
		case normalized < 0.2:
			score += 5
		case normalized < 0.4:
			score += 3
		case normalized < 0.6:
			score += 1
		}
	}

	// 5. Initialen im lokalen Teil (2 Punkte)
	if strings.Contains(email, string(lastName[0])) && strings.Contains(email, string(firstName[0])) && strings.Contains(email, string(middleName[0])) {
		score += 3
	} else if strings.Contains(email, string(firstName[0])) && strings.Contains(email, string(lastName[0])) ||
		strings.Contains(email, string(firstName[0])) && strings.Contains(email, string(middleName[0])) ||
		strings.Contains(email, string(lastName[0])) && strings.Contains(email, string(middleName[0])) {
		score += 2
	} else if strings.Contains(email, string(lastName[0])) || strings.Contains(email, string(firstName[0])) || strings.Contains(email, string(middleName[0])) {
		score += 1
	}

	return score
}

func generateInitialCombos(first, middle, last string) []string {
	var combos []string
	if first != "" && last != "" {
		combos = append(combos, string(first[0])+last, string(first[0])+"."+last)
	}
	if middle != "" && last != "" {
		combos = append(combos, string(middle[0])+last, string(middle[0])+"."+last)
	}
	if first != "" && middle != "" {
		combos = append(combos, string(first[0])+middle, string(first[0])+"."+middle)
	}
	return combos
}
