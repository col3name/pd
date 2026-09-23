package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

type req struct {
	Payload   string `json:"payload"`
	PayloadID string `json:"payload_id"`
}

// buildPayload returns a realistic PII-filled text of roughly size characters.
func buildPayload(size int) string {
	if size <= 0 {
		return "паспорт 4509 123456, email test@example.com, телефон +7 912 345-67-89"
	}
	unit := "Клиент Иванов Иван Иванович, паспорт 4509 123456, дата рождения 12.03.1998, адрес клиента: г. Москва, ул. Ленина, д. 10, тел. +7 912 345-67-89, email ivanov@test.ru, ИНН 7707083893, карта 4276 1234 5678 9012, договор № 45 от 15.03.2026. "
	unitRunes := utf8.RuneCountInString(unit)
	reps := size/unitRunes + 1
	out := strings.Repeat(unit, reps)
	r := []rune(out)
	if len(r) > size {
		out = string(r[:size])
	}
	return out
}

func main() {
	target := flag.String("target", "http://localhost:8080/process", "endpoint URL")
	concurrency := flag.Int("c", 100, "concurrency")
	duration := flag.Duration("d", 10*time.Second, "duration")
	size := flag.Int("size", 0, "payload size in characters (0 = short default payload)")
	flag.Parse()
	payload := buildPayload(*size)

	transport := &http.Transport{
		MaxIdleConns:        *concurrency * 2,
		MaxIdleConnsPerHost: *concurrency * 2,
		IdleConnTimeout:     90 * time.Second,
	}
	client := &http.Client{Timeout: 10 * time.Second, Transport: transport}
	var ok, fail int64
	var latencies []time.Duration
	var mu sync.Mutex

	start := time.Now()
	var wg sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				id := uuid.NewString()
				body, _ := json.Marshal(req{Payload: payload, PayloadID: id})
				begin := time.Now()
				httpReq, _ := http.NewRequest("POST", *target, bytes.NewReader(body))
				httpReq.Header.Set("Content-Type", "application/json")
				httpResp, err := client.Do(httpReq)
				elapsed := time.Since(begin)
				if err != nil {
					atomic.AddInt64(&fail, 1)
					continue
				}
				if httpResp.StatusCode != http.StatusOK {
					httpResp.Body.Close()
					atomic.AddInt64(&fail, 1)
					continue
				}
				_, _ = io.Copy(io.Discard, httpResp.Body)
				httpResp.Body.Close()
				atomic.AddInt64(&ok, 1)
				mu.Lock()
				latencies = append(latencies, elapsed)
				mu.Unlock()
			}
		}()
	}

	time.Sleep(*duration)
	close(stop)
	wg.Wait()

	elapsed := time.Since(start)
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	total := ok + fail
	rps := float64(total) / elapsed.Seconds()
	fmt.Printf("total=%d ok=%d fail=%d rps=%.0f\n", total, ok, fail, rps)
	if len(latencies) > 0 {
		p50 := latencies[len(latencies)/2]
		p99 := latencies[len(latencies)-1]
		if len(latencies) > 1 {
			p99 = latencies[int(float64(len(latencies))*0.99)-1]
		}
		fmt.Printf("p50=%s p99=%s\n", p50, p99)
	}
}
