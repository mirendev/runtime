package commands

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNotServingLines(t *testing.T) {
	at := time.Date(2026, 10, 8, 17, 0, 0, 0, time.UTC)
	since := "disabled since " + at.Local().Format(time.RFC1123)

	tests := []struct {
		name       string
		disabledAt *time.Time
		reason     string
		routes     []string
		wantLabel  string
		wantLines  []string
	}{
		{name: "serving", wantLabel: "", wantLines: nil},
		{
			name:      "maintenance only",
			routes:    []string{"shop.example.com"},
			wantLabel: "Maintenance:",
			wantLines: []string{"shop.example.com serving a holding page"},
		},
		{
			name:       "disabled with a reason",
			disabledAt: &at,
			reason:     "moved to the new cluster",
			wantLabel:  "Not serving:",
			wantLines:  []string{since + " — moved to the new cluster"},
		},
		{
			name:       "disabled with routes in maintenance",
			disabledAt: &at,
			routes:     []string{"shop.example.com", ""},
			wantLabel:  "Not serving:",
			wantLines: []string{
				since,
				"in maintenance instead: shop.example.com, the default route",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			label, lines := notServingLines(tt.disabledAt, tt.reason, tt.routes)
			assert.Equal(t, tt.wantLabel, label)
			assert.Equal(t, tt.wantLines, lines)
		})
	}
}
