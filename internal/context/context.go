package context

import (
	"strings"

	"github.com/kind-earthquake/pii-module/internal/detector"
)

// DefaultBoost keywords raise confidence for a PII type when present near the
// span (within Window bytes either side).
var DefaultBoost = map[detector.Type][]string{
	detector.TypeFIO:        {"клиент", "заёмщик", "владелец", "получатель", "заявитель", "паспорт", "договор", "фио", "имя", "фамилия", "отчество", "держатель", "анкета", "указал", "заявление"},
	detector.TypeAddress:    {"адрес клиента", "домашний адрес", "проживает", "зарегистрирован", "адрес регистрации", "прописан", "место жительства"},
	detector.TypeBirthPlace: {"родился", "родилась", "место рождения"},
	detector.TypeCitizenship: {"гражданство", "гражданин", "гражданка"},
	detector.TypeIssuer:     {"выдан", "выдал", "орган выдавший", "кем выдан"},
	detector.TypeCardholder: {"держатель карты", "cardholder", "имя держателя"},
}

// DefaultPenalty keywords lower confidence (bank branches, literary mentions).
var DefaultPenalty = map[detector.Type][]string{
	detector.TypeAddress: {"отделение", "офис", "банк", "филиал", "магазин", "находится по адресу"},
	detector.TypeFIO:     {"поэт", "писатель", "литературный", "написал", "роман"},
}

// Window is the default context window in bytes on each side of the span.
const Window = 100

// Resolver adjusts span confidence based on nearby keywords.
type Resolver struct {
	boost   map[detector.Type][]string
	penalty map[detector.Type][]string
	window  int
}

// New returns a Resolver. Nil maps fall back to defaults.
func New(boost, penalty map[detector.Type][]string) *Resolver {
	if boost == nil {
		boost = DefaultBoost
	}
	if penalty == nil {
		penalty = DefaultPenalty
	}
	return &Resolver{boost: boost, penalty: penalty, window: Window}
}

// Apply returns the updated confidence for s, clamped to [0,1]. It scores
// +0.2 per distinct boost keyword hit and -0.3 per distinct penalty hit.
func (r *Resolver) Apply(text string, s detector.Span) float32 {
	conf := s.Confidence
	if conf >= 1.0 && len(r.boost[s.Type]) == 0 && len(r.penalty[s.Type]) == 0 {
		return conf
	}
	from := s.Start - r.window
	if from < 0 {
		from = 0
	}
	to := s.End + r.window
	if to > len(text) {
		to = len(text)
	}
	window := strings.ToLower(text[from:to])
	b := distinctHits(window, r.boost[s.Type])
	p := distinctHits(window, r.penalty[s.Type])
	conf += float32(b) * 0.2
	conf -= float32(p) * 0.3
	if conf > 1.0 {
		return 1.0
	}
	if conf < 0.0 {
		return 0.0
	}
	return conf
}

// distinctHits counts distinct keywords present in the window.
func distinctHits(window string, keywords []string) int {
	n := 0
	for _, k := range keywords {
		if strings.Contains(window, k) {
			n++
		}
	}
	return n
}