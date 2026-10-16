package httpingress

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/errorpage"
)

// TestDocsErrorPageTemplate checks that the sample template on the server
// config page parses and renders, so the copy readers paste stays working.
func TestDocsErrorPageTemplate(t *testing.T) {
	doc, err := os.ReadFile("../../docs/docs/server-config.md")
	require.NoError(t, err)
	_, after, ok := strings.Cut(string(doc), "Copy this into `/etc/miren/error.html`")
	require.True(t, ok)
	_, after, ok = strings.Cut(after, "```html\n")
	require.True(t, ok)
	source, _, ok := strings.Cut(after, "\n```")
	require.True(t, ok)
	page, err := parseErrorTemplate(source)
	require.NoError(t, err)
	_, err = errorpage.ParseApp(source, brandLogo)
	require.NoError(t, err)

	for _, tt := range []struct {
		name string
		data errorPageData
		want string
	}{
		{"ordinary error", errorPageData{Status: 502, Title: "Unavailable", Description: "Try later"}, "<h1>Unavailable</h1>"},
		{"maintenance", errorPageData{Status: 503, Site: "shop.test", Reason: "Upgrading", BackAt: "12:00 UTC", Maintenance: true}, "shop.test is down for maintenance"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := string(renderErrorPage(page, nil, tt.data))
			assert.Contains(t, body, tt.want)
			assert.NotContains(t, body, "{{")
			assert.NotContains(t, body, "Powered by Miren")
		})
	}
}
