package detector

import (
	"strings"
)

// firstNames is a small dictionary of common Russian first names.
var firstNames = map[string]bool{
	"иван": true, "александр": true, "пётр": true, "сергей": true,
	"дмитрий": true, "андрей": true, "алексей": true, "николай": true,
	"михаил": true, "владимир": true, "павел": true, "артём": true,
	"максим": true, "елена": true, "ольга": true, "мария": true,
	"анна": true, "наталья": true, "татьяна": true, "ирина": true,
	"светлана": true,
}

// cities is a small dictionary of common Russian cities.
var cities = map[string]bool{
	"москва": true, "санкт-петербург": true, "новосибирск": true,
	"екатеринбург": true, "казань": true, "нижний новгород": true,
	"челябинск": true, "самара": true, "омск": true, "ростов-на-дону": true,
	"уфа": true, "красноярск": true, "воронеж": true, "пермь": true,
	"волгоград": true,
}

// surnameSuffixes are common Russian surname endings.
var surnameSuffixes = []string{"ова", "ева", "ина", "ская", "цкая", "ов", "ев", "ин", "ский", "цкий", "ко", "чук", "енко"}

// isSurname reports whether word looks like a Russian surname.
func isSurname(word string) bool {
	lower := strings.ToLower(word)
	for _, s := range surnameSuffixes {
		if strings.HasSuffix(lower, s) && len(lower) > len(s)+1 {
			return true
		}
	}
	return false
}

// contextKeywords returns the context keywords that raise confidence for t.
func contextKeywords(t Type) []string {
	switch t {
	case TypeFIO:
		return []string{"клиент", "заёмщик", "владелец", "получатель", "заявитель",
			"паспорт", "договор", "фио", "имя", "фамилия", "отчество", "держатель"}
	case TypeAddress:
		return []string{"адрес клиента", "домашний адрес", "проживает", "зарегистрирован",
			"адрес регистрации", "прописан", "место жительства"}
	case TypeBirthPlace:
		return []string{"родился в", "родилась в", "место рождения"}
	case TypeCitizenship:
		return []string{"гражданство", "гражданин", "гражданка"}
	case TypeIssuer:
		return []string{"выдан", "выдал", "орган выдавший", "кем выдан"}
	case TypeCardholder:
		return []string{"держатель карты", "cardholder", "имя держателя"}
	default:
		return nil
	}
}

// negativeContext returns the context keywords that lower confidence for t.
func negativeContext(t Type) []string {
	switch t {
	case TypeAddress:
		return []string{"отделение", "офис", "банк", "филиал", "магазин", "находится по адресу"}
	default:
		return nil
	}
}

// hasContext reports whether any keyword appears in a window around [start,end).
func hasContext(text string, start, end int, keywords []string) bool {
	return hasAnyKeyword(text, start, end, keywords)
}

// hasNegativeContext reports whether any negative keyword appears in a window.
func hasNegativeContext(text string, start, end int, keywords []string) bool {
	return hasAnyKeyword(text, start, end, keywords)
}

func hasAnyKeyword(text string, start, end int, keywords []string) bool {
	if len(keywords) == 0 {
		return false
	}
	from := start - 80
	if from < 0 {
		from = 0
	}
	to := end + 80
	if to > len(text) {
		to = len(text)
	}
	window := strings.ToLower(text[from:to])
	for _, k := range keywords {
		if strings.Contains(window, k) {
			return true
		}
	}
	return false
}

// scoreConfidence computes the additive confidence score, clamped to [0,1].
func scoreConfidence(base float32, dictHit bool, ctxHits, negHits int) float32 {
	score := base
	if dictHit {
		score += 0.3
	}
	score += float32(ctxHits) * 0.2
	if score > 1.0 {
		score = 1.0
	}
	score -= float32(negHits) * 0.3
	if score < 0.0 {
		score = 0.0
	}
	return score
}