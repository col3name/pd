package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type prometheusConfig struct {
	ScrapeConfigs []struct {
		JobName       string `yaml:"job_name"`
		StaticConfigs []struct {
			Targets []string `yaml:"targets"`
		} `yaml:"static_configs"`
	} `yaml:"scrape_configs"`
}

type composeConfig struct {
	Services map[string]struct {
		Volumes []string `yaml:"volumes"`
	} `yaml:"services"`
	Volumes map[string]struct {
		Name string `yaml:"name"`
	} `yaml:"volumes"`
}

func loadYAMLForDeploymentTest(t *testing.T, path string, destination any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", path))
	require.NoError(t, err)
	require.NoError(t, yaml.Unmarshal(data, destination))
}

func TestComposePrometheusTargetsComposeService(t *testing.T) {
	var cfg prometheusConfig
	loadYAMLForDeploymentTest(t, "prometheus.yml", &cfg)
	require.Len(t, cfg.ScrapeConfigs, 1)
	require.Equal(t, "pii", cfg.ScrapeConfigs[0].JobName)
	require.Len(t, cfg.ScrapeConfigs[0].StaticConfigs, 1)
	require.Equal(t, []string{"pii-module-v2:8080"}, cfg.ScrapeConfigs[0].StaticConfigs[0].Targets)
}

func TestSwarmPrometheusTargetsSwarmServices(t *testing.T) {
	var cfg prometheusConfig
	loadYAMLForDeploymentTest(t, filepath.Join("deploy", "prometheus.yml"), &cfg)
	require.Len(t, cfg.ScrapeConfigs, 2)
	require.Equal(t, "pii", cfg.ScrapeConfigs[0].JobName)
	require.Len(t, cfg.ScrapeConfigs[0].StaticConfigs, 1)
	require.Equal(t, []string{"pii:8080"}, cfg.ScrapeConfigs[0].StaticConfigs[0].Targets)
	require.Equal(t, "cadvisor", cfg.ScrapeConfigs[1].JobName)
	require.Len(t, cfg.ScrapeConfigs[1].StaticConfigs, 1)
	require.Equal(t, []string{"cadvisor:8080"}, cfg.ScrapeConfigs[1].StaticConfigs[0].Targets)
}

func TestSwarmPrometheusMountsSiblingConfig(t *testing.T) {
	var cfg composeConfig
	loadYAMLForDeploymentTest(t, filepath.Join("deploy", "stack.yml"), &cfg)
	prometheus, ok := cfg.Services["prometheus"]
	require.True(t, ok)
	require.Contains(t, prometheus.Volumes, "./prometheus.yml:/etc/prometheus/prometheus.yml:ro")
	require.Contains(t, cfg.Services, "pii")
	require.Contains(t, cfg.Services, "cadvisor")
}

func TestComposePrometheusUsesNamedVolume(t *testing.T) {
	var cfg composeConfig
	loadYAMLForDeploymentTest(t, "docker-compose.yml", &cfg)
	prometheus, ok := cfg.Services["prometheus"]
	require.True(t, ok)
	require.Contains(t, prometheus.Volumes, "promdata:/prometheus")
	volume, ok := cfg.Volumes["promdata"]
	require.True(t, ok)
	require.Equal(t, "pii-compose-prometheus-data", volume.Name)
}

func TestComposeDoesNotRunCadvisor(t *testing.T) {
	var cfg composeConfig
	loadYAMLForDeploymentTest(t, "docker-compose.yml", &cfg)
	require.NotContains(t, cfg.Services, "cadvisor")
}
