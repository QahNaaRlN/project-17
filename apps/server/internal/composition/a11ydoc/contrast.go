package a11ydoc

import (
	"math"
	"strconv"
	"strings"

	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
)

type color struct {
	r, g, b, a float64
	known, raw bool
}

func parseColor(s string) color {
	switch strings.ToLower(s) {
	case "black":
		s = "#000000"
	case "white":
		s = "#ffffff"
	case "red":
		s = "#ff0000"
	case "transparent":
		return color{a: 0, known: true}
	}
	if !strings.HasPrefix(s, "#") {
		return color{}
	}
	s = s[1:]
	if len(s) == 3 || len(s) == 4 {
		var out strings.Builder
		for _, ch := range s {
			out.WriteRune(ch)
			out.WriteRune(ch)
		}
		s = out.String()
	}
	if len(s) != 6 && len(s) != 8 {
		return color{}
	}
	n, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return color{}
	}
	a := 1.0
	if len(s) == 8 {
		a = float64(n&255) / 255
		n >>= 8
	}
	return color{float64((n>>16)&255) / 255, float64((n>>8)&255) / 255, float64(n&255) / 255, a, true, false}
}
func over(f, b color) color {
	if !f.known {
		return f
	}
	if f.a == 1 {
		return f
	}
	if !b.known || b.a != 1 {
		return color{raw: f.raw || b.raw}
	}
	return color{f.r*f.a + b.r*(1-f.a), f.g*f.a + b.g*(1-f.a), f.b*f.a + b.b*(1-f.a), 1, true, f.raw || b.raw}
}

// W3C G18: no rounding before comparing against the threshold.
func luminance(c color) float64 {
	linear := func(n float64) float64 {
		if n <= 0.04045 {
			return n / 12.92
		}
		return math.Pow((n+0.055)/1.055, 2.4)
	}
	return .2126*linear(c.r) + .7152*linear(c.g) + .0722*linear(c.b)
}
func ratio(a, b color) float64 {
	x, y := luminance(a), luminance(b)
	return (max(x, y) + .05) / (min(x, y) + .05)
}
func responsive(v any, bp string) any {
	m := obj(v)
	if m != nil {
		if raw, ok := m["raw"]; ok {
			return map[string]any{"raw": raw}
		}
		if v, ok := m[bp]; ok {
			return v
		}
		return m["base"]
	}
	return v
}
func (c *checker) color(v any, theme string) color {
	if m := obj(v); m != nil {
		if s, ok := m["raw"].(string); ok {
			v := parseColor(s)
			v.raw = true
			return v
		}
		return color{}
	}
	name, ok := v.(string)
	if !ok {
		return color{}
	}
	token := obj(obj(c.app["tokens"])["colors"])[name]
	if m := obj(token); m != nil {
		token = m["value"]
		if v, ok := obj(m["modes"])[theme]; ok {
			token = v
		}
	}
	s, _ := token.(string)
	return parseColor(s)
}
func (c *checker) contrasts(root *view) {
	bps := append([]string{"base"}, keys(obj(c.app["breakpoints"]))...)
	themes := map[string]any{"base": true}
	for _, v := range obj(obj(c.app["tokens"])["colors"]) {
		for name := range obj(obj(v)["modes"]) {
			themes[name] = true
		}
	}
	states := map[string]any{"": true}
	var collect func(*view)
	collect = func(v *view) {
		for name := range obj(obj(v.node["design"])["states"]) {
			states[name] = true
		}
		for _, child := range v.children {
			collect(child)
		}
	}
	collect(root)
	for _, state := range keys(states) {
		for _, bp := range bps {
			for _, theme := range keys(themes) {
				var walk func(*view, color, color, any, bool)
				walk = func(v *view, fg, bg color, typography any, active bool) {
					base := obj(v.node["design"])
					if responsive(base["hidden"], bp) == true {
						return
					}
					d := map[string]any{}
					for k, val := range base {
						d[k] = val
					}
					if override := obj(obj(base["states"])[state]); override != nil {
						active = true
						for k, val := range override {
							d[k] = val
						}
					}
					if val, ok := d["color"]; ok {
						fg = c.color(responsive(val, bp), theme)
					}
					if val, ok := d["background"]; ok {
						bg = over(c.color(responsive(val, bp), theme), bg)
					}
					if val, ok := d["typography"]; ok {
						typography = responsive(val, bp)
					}
					if d["backgroundImage"] != nil {
						bg = color{}
					}
					minimum := 4.5
					if name, ok := typography.(string); ok && obj(obj(obj(c.app["tokens"])["typography"])[name])["large"] == true {
						minimum = 3
					}
					check := func(f, b color, state string) {
						if !f.known || !b.known || b.a != 1 {
							return
						}
						f = over(f, b)
						if !f.known {
							return
						}
						r := ratio(f, b)
						if r >= minimum {
							return
						}
						severity := ir.SeverityError
						if f.raw || b.raw {
							severity = ir.SeverityWarning
						}
						field := "design/color"
						if state != "" {
							field = "design/states/" + state + "/color"
						}
						c.add(v, "A11Y_CONTRAST", field, severity, map[string]any{"ratio": r, "minimum": minimum, "breakpoint": bp, "theme": theme, "state": state})
					}
					switch v.node["type"] {
					case "Heading", "Text", "RichText", "Label", "Button", "Link":
						if state == "" || active {
							check(fg, bg, state)
						}
					}
					for _, child := range v.children {
						walk(child, fg, bg, typography, active)
					}
				}
				walk(root, color{}, color{}, nil, false)
			}
		}
	}
}
