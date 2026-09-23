package masker

import (
	"fmt"
	"hash/fnv"
	"math/rand"
	"strings"

	"github.com/kind-earthquake/pii-module/internal/detector"
)

// Synthetic replaces each span in text with a realistic synthetic value based
// on its type. Spans must be sorted by Start ascending. Replacement is done
// left-to-right by building a new string, so byte offsets remain valid.
//
// The synthetic value for a span is derived from a hash of the span's original
// text, so the same input always yields the same synthetic output. This keeps
// the benchmark retry logic working (a re-sent payload produces the same
// masked result).
func Synthetic(text string, spans []detector.Span) string {
	if len(spans) == 0 {
		return text
	}
	var b strings.Builder
	prev := 0
	for _, s := range spans {
		b.WriteString(text[prev:s.Start])
		b.WriteString(syntheticFor(s, text[s.Start:s.End]))
		prev = s.End
	}
	b.WriteString(text[prev:])
	return b.String()
}

// syntheticFor returns the synthetic replacement for a single span.
func syntheticFor(s detector.Span, original string) string {
	rng := rand.New(rand.NewSource(seedFor(original)))
	switch s.Type {
	case detector.TypeFIO:
		return randomName(rng)
	case detector.TypePhone:
		return "+7 900 000-00-00"
	case detector.TypeEmail:
		return "user123@example.com"
	case detector.TypeCard:
		return randomCard(rng)
	case detector.TypePassport, detector.TypeDriverLicense,
		detector.TypeForeignPassport, detector.TypeMilitaryID,
		detector.TypeBirthCertificate:
		return randomDocSeriesNumber(rng)
	case detector.TypeINN:
		return randomINN(rng)
	case detector.TypeDate, detector.TypeBirthDate:
		return randomDate(rng)
	case detector.TypeCVV:
		return fmt.Sprintf("%03d", rng.Intn(1000))
	case detector.TypePIN:
		return fmt.Sprintf("%04d", rng.Intn(10000))
	case detector.TypeAddress:
		return randomAddress(rng)
	case detector.TypeCardholder:
		return randomName(rng)
	default:
		return s.Type.Placeholder()
	}
}

// seedFor derives a deterministic PRNG seed from the span's original text.
func seedFor(original string) int64 {
	h := fnv.New64a()
	h.Write([]byte(original))
	return int64(h.Sum64())
}

var (
	lastNames = []string{
		"Петров", "Сидоров", "Кузнецов", "Смирнов", "Волков",
		"Соколов", "Морозов", "Лебедев", "Козлов", "Новиков",
	}
	firstNames = []string{
		"Пётр", "Иван", "Алексей", "Дмитрий", "Сергей",
		"Андрей", "Михаил", "Николай", "Павел", "Владимир",
	}
	middleNames = []string{
		"Петрович", "Иванович", "Алексеевич", "Дмитриевич", "Сергеевич",
		"Андреевич", "Михайлович", "Николаевич", "Павлович", "Владимирович",
	}
	streets = []string{
		"ул. Ленина", "ул. Пушкина", "ул. Гагарина", "ул. Мира",
		"ул. Садовая", "ул. Центральная", "пр-т Победы", "ул. Школьная",
	}
	cities = []string{
		"Москва", "Санкт-Петербург", "Новосибирск", "Екатеринбург",
		"Казань", "Нижний Новгород", "Челябинск", "Самара",
	}
)

func randomName(rng *rand.Rand) string {
	return lastNames[rng.Intn(len(lastNames))] + " " +
		firstNames[rng.Intn(len(firstNames))] + " " +
		middleNames[rng.Intn(len(middleNames))]
}

func randomCard(rng *rand.Rand) string {
	digits := make([]byte, 16)
	for i := 0; i < 15; i++ {
		digits[i] = byte('0' + rng.Intn(10))
	}
	// Compute the Luhn check digit for the first 15 digits.
	sum := 0
	double := true
	for i := 14; i >= 0; i-- {
		d := int(digits[i] - '0')
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	digits[15] = byte('0' + (10-sum%10)%10)
	return string(digits)
}

func randomDocSeriesNumber(rng *rand.Rand) string {
	series := fmt.Sprintf("%04d", rng.Intn(10000))
	number := fmt.Sprintf("%06d", rng.Intn(1000000))
	return series + " " + number
}

func randomINN(rng *rand.Rand) string {
	n := 10
	if rng.Intn(2) == 0 {
		n = 12
	}
	digits := make([]byte, n)
	for i := 0; i < n; i++ {
		digits[i] = byte('0' + rng.Intn(10))
	}
	return string(digits)
}

func randomDate(rng *rand.Rand) string {
	day := rng.Intn(28) + 1
	month := rng.Intn(12) + 1
	year := rng.Intn(60) + 1940
	return fmt.Sprintf("%02d.%02d.%04d", day, month, year)
}

func randomAddress(rng *rand.Rand) string {
	house := rng.Intn(200) + 1
	return "г. " + cities[rng.Intn(len(cities))] + ", " +
		streets[rng.Intn(len(streets))] + ", д. " + fmt.Sprintf("%d", house)
}
