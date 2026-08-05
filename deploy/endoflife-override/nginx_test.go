package override

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNginxSourceHeaders(t *testing.T) {
	data, err := os.ReadFile("nginx.conf")
	require.NoError(t, err)
	config := string(data)

	local := locationBlock(t, config, "location /api/ {")
	upstream := locationBlock(t, config, "location @upstream {")
	assert.Equal(t, 1, strings.Count(local, "add_header X-Version-Guard-EOL-Source local_override always;"))
	assert.Equal(t, 1, strings.Count(upstream, "proxy_hide_header X-Version-Guard-EOL-Source;"))
	assert.Equal(t, 1, strings.Count(upstream, "add_header X-Version-Guard-EOL-Source endoflife_date always;"))
	assert.Equal(t, 2, strings.Count(config, "add_header X-Version-Guard-EOL-Source"))
}

func locationBlock(t *testing.T, config, start string) string {
	t.Helper()
	startIndex := strings.Index(config, start)
	require.NotEqual(t, -1, startIndex)
	remainder := config[startIndex+len(start):]
	endIndex := strings.Index(remainder, "\n    }")
	require.NotEqual(t, -1, endIndex)
	return remainder[:endIndex]
}
