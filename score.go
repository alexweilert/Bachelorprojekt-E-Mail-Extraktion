package main

import "strings"

func getScore(email, firstName, middleName, lastName string) int {
	email = strings.ToLower(email)
	score := 0

	if strings.Contains(firstName, "ä") {
		strings.Replace(email, "ä", "ae", 1)
	}

	if strings.Contains(email, "ö") {
		strings.Replace(email, "ö", "oe", 1)
	}

	// 1. Direkte Matches (4 Punkte)
	if firstName != "" && strings.Contains(email, firstName) {
		score += 4
	}
	if middleName != "" && strings.Contains(email, middleName) {
		score += 4
	}
	if lastName != "" && strings.Contains(email, lastName) {
		score += 4
	}

	// 2. Initial + Nachname
	initialCombos := []string{}

	if firstName != "" && lastName != "" {
		initialCombos = append(initialCombos,
			string(firstName[0])+lastName,
			string(firstName[0])+"."+lastName,
		)
	}
	if middleName != "" && lastName != "" {
		initialCombos = append(initialCombos,
			string(middleName[0])+lastName,
			string(middleName[0])+"."+lastName,
		)
	}
	if firstName != "" && middleName != "" {
		initialCombos = append(initialCombos,
			string(firstName[0])+middleName,
			string(firstName[0])+"."+middleName,
		)
	}
	if lastName != "" && middleName != "" {
		initialCombos = append(initialCombos,
			string(lastName[0])+middleName,
			string(lastName[0])+"."+middleName,
		)
	}
	if lastName != "" && firstName != "" {
		initialCombos = append(initialCombos,
			string(lastName[0])+firstName,
			string(lastName[0])+"."+firstName,
		)
	}

	for _, combo := range initialCombos {
		if strings.Contains(email, combo) {
			score += 3
		}
	}

	// 3. Kürzere Teilstücke (2 Punkte)
	//if len(middleName) >= 2 && strings.Contains(email, middleName[:2]) {
	//	score += 3
	//}
	if len(lastName) >= 4 && strings.Contains(email, lastName[:4]) {
		score += 3
	}
	if len(firstName) >= 3 && strings.Contains(email, firstName[:3]) {
		score += 3
	}

	// 4. Initial + gekürzter Nachname
	if firstName != "" && lastName != "" {
		initial := string(firstName[0])
		for i := 4; i <= len(lastName); i++ {
			if strings.Contains(email, initial+lastName[:i]) {
				score += 3
				break
			}
		}
	}
	if firstName != "" && middleName != "" {
		initial := string(firstName[0])
		for i := 4; i <= len(middleName); i++ {
			if strings.Contains(email, initial+middleName[:i]) {
				score += 3
				break
			}
		}
	}
	if middleName != "" && lastName != "" {
		initial := string(middleName[0])
		for i := 4; i <= len(lastName); i++ {
			if strings.Contains(email, initial+lastName[:i]) {
				score += 3
				break
			}
		}
	}

	// 5. Volle Initialenkombination
	fullInitials := ""
	if firstName != "" {
		fullInitials += string(firstName[0])
	}
	if len(fullInitials) == 1 && strings.Contains(email, fullInitials) {
		score += 1
	}
	if middleName != "" {
		fullInitials += string(middleName[0])
	}
	if len(fullInitials) == 2 && strings.Contains(email, fullInitials) {
		score += 1
	}
	if lastName != "" {
		fullInitials += string(lastName[0])
	}
	if len(fullInitials) == 3 && strings.Contains(email, fullInitials) {
		score += 1
	}

	if len(fullInitials) >= 2 && strings.Contains(email, fullInitials) {
		score += 4
	}
	return score
}
