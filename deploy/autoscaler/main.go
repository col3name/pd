package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"time"
)

type cfg struct {
	promURL       string
	service       string
	minReplicas   int
	maxReplicas   int
	cpuUp         float64
	cpuDown       float64
	queueUp       float64
	latencyP99MS  float64
	cooldown      time.Duration
	poll          time.Duration
}

func load() cfg {
	atoi := func(k string, def int) int {
		if v := os.Getenv(k); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				return n
			}
		}
		return def
	}
	atof := func(k string, def float64) float64 {
		if v := os.Getenv(k); v != "" {
			if n, err := strconv.ParseFloat(v, 64); err == nil {
				return n
			}
		}
		return def
	}
	return cfg{
		promURL:      os.Getenv("PROM_URL"),
		service:      os.Getenv("SERVICE"),
		minReplicas:  atoi("MIN_REPLICAS", 2),
		maxReplicas:  atoi("MAX_REPLICAS", 20),
		cpuUp:        atof("CPU_UP", 70),
		cpuDown:      atof("CPU_DOWN", 30),
		queueUp:      atof("QUEUE_UP", 100),
		latencyP99MS: atof("LATENCY_P99_MS", 100),
		cooldown:     time.Duration(atoi("COOLDOWN_SECONDS", 30)) * time.Second,
		poll:         time.Duration(atoi("POLL_SECONDS", 10)) * time.Second,
	}
}

func queryFloat(ctx context.Context, url string) (float64, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out struct {
		Data struct {
			Result []struct {
				Value []interface{} `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return 0, err
	}
	if len(out.Data.Result) == 0 || len(out.Data.Result[0].Value) < 2 {
		return 0, fmt.Errorf("no data")
	}
	return strconv.ParseFloat(out.Data.Result[0].Value[1].(string), 64)
}

func currentReplicas(ctx context.Context, service string) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", "service", "inspect", "--format", "{{.Spec.Mode.Replicated.Replicas}}", service)
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(string(out))
}

func scale(ctx context.Context, service string, replicas int) error {
	cmd := exec.CommandContext(ctx, "docker", "service", "scale", fmt.Sprintf("%s=%d", service, replicas))
	return cmd.Run()
}

func main() {
	c := load()
	lastScale := time.Time{}
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		queueDepth, _ := queryFloat(ctx, c.promURL+"/api/v1/query?query=pii_queue_depth")
		latency, _ := queryFloat(ctx, c.promURL+"/api/v1/query?query=histogram_quantile(0.99, sum(rate(pii_latency_seconds_bucket[1m])) by (le))")
		cancel()

		replicas, err := currentReplicas(context.Background(), c.service)
		if err != nil {
			fmt.Fprintf(os.Stderr, "autoscaler: inspect: %v\n", err)
			time.Sleep(c.poll)
			continue
		}

		scaleUp := queueDepth > c.queueUp || latency > c.latencyP99MS
		scaleDown := queueDepth < c.queueUp/10 && latency < c.latencyP99MS/10

		if scaleUp && replicas < c.maxReplicas && time.Since(lastScale) > c.cooldown {
			fmt.Printf("autoscaler: scale up %s %d->%d (queue=%.0f latency=%.0fms)\n", c.service, replicas, replicas+1, queueDepth, latency)
			if err := scale(context.Background(), c.service, replicas+1); err != nil {
				fmt.Fprintf(os.Stderr, "autoscaler: scale up: %v\n", err)
			} else {
				lastScale = time.Now()
			}
		} else if scaleDown && replicas > c.minReplicas && time.Since(lastScale) > c.cooldown {
			fmt.Printf("autoscaler: scale down %s %d->%d\n", c.service, replicas, replicas-1)
			if err := scale(context.Background(), c.service, replicas-1); err != nil {
				fmt.Fprintf(os.Stderr, "autoscaler: scale down: %v\n", err)
			} else {
				lastScale = time.Now()
			}
		}
		time.Sleep(c.poll)
	}
}