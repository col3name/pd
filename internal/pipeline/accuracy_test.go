package pipeline_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

type caseFile struct {
	Text string   `json:"text"`
	Mask []string `json:"mask"`
}

func loadCases(t *testing.T, name string) []caseFile {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", name))
	require.NoError(t, err)
	var out []caseFile
	require.NoError(t, json.Unmarshal(data, &out))
	return out
}

func TestAccuracyDataset(t *testing.T) {
	p := newPipeline("redact")

	// Positive cases: every listed placeholder must appear in the mask output.
	pos := loadCases(t, "pii_cases.json")
	require.NotEmpty(t, pos)
	for _, c := range pos {
		res := p.Process(c.Text)
		require.NotEmpty(t, c.Mask, "case %q must declare expected placeholders", c.Text)
		for _, m := range c.Mask {
			require.Contains(t, res.Masked, m, "%q must contain %s", c.Text, m)
		}
	}

	// Negative cases: text must be returned byte-for-byte unmasked.
	neg := loadCases(t, "false_positives.json")
	require.NotEmpty(t, neg)
	for _, c := range neg {
		require.Equal(t, c.Text, p.Process(c.Text).Masked, "%q must stay unmasked", c.Text)
	}
}
