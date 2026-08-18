package httpserver

import (
	"fmt"
	"strings"
)

// ring appends v to buf, keeping at most n most-recent samples.
func ring(buf []float64, v float64, n int) []float64 {
	buf = append(buf, v)
	if len(buf) > n {
		buf = buf[len(buf)-n:]
	}
	return buf
}

// sparkline renders a small auto-scaled SVG line chart of the values. Auto
// scaling (min..max of the window) makes even small variations visibly move,
// which is what sells the "live" feel.
func sparkline(vals []float64) string {
	const w, h = 120, 32
	if len(vals) < 2 {
		return fmt.Sprintf(`<svg class="spark" viewBox="0 0 %d %d" preserveAspectRatio="none"></svg>`, w, h)
	}
	mn, mx := vals[0], vals[0]
	for _, v := range vals {
		if v < mn {
			mn = v
		}
		if v > mx {
			mx = v
		}
	}
	rng := mx - mn
	if rng <= 0 {
		rng = 1 // flat line sits mid-height
	}
	step := float64(w) / float64(len(vals)-1)
	var b strings.Builder
	for i, v := range vals {
		x := float64(i) * step
		// invert Y (SVG origin top-left); pad 3px top/bottom
		y := float64(h-3) - ((v-mn)/rng)*float64(h-6)
		fmt.Fprintf(&b, "%.1f,%.1f ", x, y)
	}
	pts := strings.TrimSpace(b.String())
	return fmt.Sprintf(`<svg class="spark" viewBox="0 0 %d %d" preserveAspectRatio="none"><polyline points="%s"/></svg>`, w, h, pts)
}

// metricBody is the swapped-in body of a metric tile: the current value plus a
// sparkline. Rendered on one line (SSE data is single-line).
func metricBody(value, spark string) string {
	return `<div class="metric-val">` + value + `</div>` + spark
}
