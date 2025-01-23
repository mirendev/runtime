package colortheory

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

func ChangeLightness(hex string, change float64) string {
	hex = strings.TrimPrefix(hex, "#")

	// Hex to RGB
	r, _ := strconv.ParseInt(hex[0:2], 16, 64)
	g, _ := strconv.ParseInt(hex[2:4], 16, 64)
	b, _ := strconv.ParseInt(hex[4:6], 16, 64)

	// RGB to HSL
	r_norm := float64(r) / 255
	g_norm := float64(g) / 255
	b_norm := float64(b) / 255

	max := math.Max(math.Max(r_norm, g_norm), b_norm)
	min := math.Min(math.Min(r_norm, g_norm), b_norm)

	h, s, l := 0.0, 0.0, (max+min)/2

	if max != min {
		d := max - min
		s = d / (2 - max - min)
		if l > 0.5 {
			s = d / (max + min)
		}

		switch max {
		case r_norm:
			h = (g_norm - b_norm) / d
			if g_norm < b_norm {
				h += 6
			}
		case g_norm:
			h = (b_norm-r_norm)/d + 2
		case b_norm:
			h = (r_norm-g_norm)/d + 4
		}
		h /= 6
	}

	// Darken by reducing lightness
	l *= 1.0 - (change / 100.0)

	// HSL to RGB
	var r2, g2, b2 float64

	if s == 0 {
		r2, g2, b2 = l, l, l
	} else {
		var q float64
		if l < 0.5 {
			q = l * (1 + s)
		} else {
			q = l + s - l*s
		}
		p := 2*l - q

		r2 = hueToRGB(p, q, h+1.0/3.0)
		g2 = hueToRGB(p, q, h)
		b2 = hueToRGB(p, q, h-1.0/3.0)
	}

	return fmt.Sprintf("#%02x%02x%02x",
		int64(r2*255),
		int64(g2*255),
		int64(b2*255))
}

func hueToRGB(p, q, t float64) float64 {
	if t < 0 {
		t += 1
	}
	if t > 1 {
		t -= 1
	}
	if t < 1.0/6.0 {
		return p + (q-p)*6*t
	}
	if t < 1.0/2.0 {
		return q
	}
	if t < 2.0/3.0 {
		return p + (q-p)*(2.0/3.0-t)*6
	}
	return p
}
