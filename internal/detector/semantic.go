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

// detectFIO finds FIO spans (consecutive name/surname tokens) and scores them.
func detectFIO(text string) []Span {
	words := tokenize(text)
	var spans []Span
	for i := 0; i < len(words); i++ {
		w := words[i]
		if !isNameToken(w.text) {
			continue
		}
		// Extend over consecutive name tokens.
		j := i
		for j+1 < len(words) && isNameToken(words[j+1].text) {
			j++
		}
		start := words[i].start
		end := words[j].end
		dictHit := true
		ctxHits := 0
		if hasContext(text, start, end, contextKeywords(TypeFIO)) {
			ctxHits = 1
		}
		conf := scoreConfidence(0.5, dictHit, ctxHits, 0)
		spans = append(spans, Span{Start: start, End: end, Type: TypeFIO, Confidence: conf})
		i = j
	}
	return spans
}

// word is a token with its byte offsets in the source text.
type word struct {
	text  string
	start int
	end   int
}

// tokenize splits text into words with byte offsets.
func tokenize(text string) []word {
	var words []word
	start := -1
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c == ' ' || c == '\t' || c == '\n' || c == ',' || c == '.' ||
			c == ':' || c == ';' || c == '(' || c == ')' || c == '-' {
			if start >= 0 {
				words = append(words, word{text: text[start:i], start: start, end: i})
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		words = append(words, word{text: text[start:], start: start, end: len(text)})
	}
	return words
}

// isNameToken reports whether a word is a first name or surname.
func isNameToken(w string) bool {
	lower := strings.ToLower(w)
	return firstNames[lower] || isSurname(w)
}