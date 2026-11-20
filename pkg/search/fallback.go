package search

import (
	"slices"
	"strings"
	"unicode"
)

// fallbackTerms retains literal identifiers and corrects only unambiguous
// words found in the selected corpus.
func fallbackTerms(query string, terms []string, fields []Fields) []string {
	for _, term := range terms {
		if strings.IndexFunc(term, func(r rune) bool { return !unicode.IsLetter(r) }) >= 0 {
			return nil
		}
	}
	for _, word := range strings.Fields(query) {
		if camelCase(word) {
			return nil
		}
	}
	vocabulary := map[string]bool{}
	addWords := func(text string) {
		for _, word := range strings.FieldsFunc(text, func(r rune) bool { return !isWordRune(r) }) {
			if !camelCase(word) && strings.IndexFunc(word, unicode.IsDigit) < 0 {
				vocabulary[strings.ToLower(word)] = true
			}
		}
	}
	for _, doc := range fields {
		addWords(doc.Title)
		addWords(doc.Description)
		addWords(doc.Body)
		for _, tag := range doc.Tags {
			addWords(tag)
		}
	}
	corrected := make([]string, 0, len(terms))
	changed := false
	for _, term := range terms {
		guess := correction(term, vocabulary)
		if guess != "" && guess != term {
			term = guess
			changed = true
		}
		if !slices.Contains(corrected, term) {
			corrected = append(corrected, term)
		}
	}
	if changed {
		return corrected
	}
	return nil
}

func correction(term string, vocabulary map[string]bool) string {
	if vocabulary[term] {
		return term
	}
	for _, form := range simplerWordForms(term) {
		if vocabulary[form] {
			return form
		}
	}
	letters := []rune(term)
	if len(letters) < 6 {
		return ""
	}
	guess := ""
	for word := range vocabulary {
		if !oneEditApart(letters, []rune(word)) {
			continue
		}
		if guess != "" {
			return ""
		}
		guess = word
	}
	return guess
}

func simplerWordForms(word string) []string {
	for _, suffix := range []string{"ing", "ied", "ies", "ed", "es", "s"} {
		root, found := strings.CutSuffix(word, suffix)
		if !found || len([]rune(root)) < 3 {
			continue
		}
		switch suffix {
		case "ied", "ies":
			return []string{root + "y"}
		case "ing", "ed":
			forms := []string{root, root + "e"}
			letters := []rune(root)
			if len(letters) > 3 && letters[len(letters)-1] == letters[len(letters)-2] {
				forms = append(forms, string(letters[:len(letters)-1]))
			}
			return forms
		case "es":
			return []string{root, root + "e"}
		default:
			return []string{root}
		}
	}
	return nil
}

func camelCase(word string) bool {
	lower := false
	for _, r := range word {
		if lower && unicode.IsUpper(r) {
			return true
		}
		lower = lower || unicode.IsLower(r)
	}
	return false
}

func oneEditApart(a, b []rune) bool {
	if len(a) > len(b) {
		a, b = b, a
	}
	extra := len(b) - len(a)
	if extra > 1 {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return slices.Equal(a[i+1-extra:], b[i+1:])
		}
	}
	return extra == 1
}
