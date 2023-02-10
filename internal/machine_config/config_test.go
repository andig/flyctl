package machine_config

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetAndSetEnvVariables(t *testing.T) {
	cfg := NewConfig()
	cfg.SetEnvVariable("A", "B")
	cfg.SetEnvVariable("C", "D")
	assert.Equal(t, map[string]string{"A": "B", "C": "D"}, cfg.Env)

	buf := &bytes.Buffer{}
	if err := cfg.marshalTOML(buf); err != nil {
		assert.NoError(t, err)
	}
	cfg2, err := unmarshalTOML(buf.Bytes())
	assert.NoError(t, err)
	assert.Equal(t, cfg.Env, cfg2.Env)
}
