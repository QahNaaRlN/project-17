package policydoc

import (
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
)

type rule struct {
	token, values, applies string
	placement              bool
	minimum                Mode
	raw                    string
	low, high              float64
	units                  string
}

// Placement fields are the only design fields exposed in STRICT.
var catalogue = map[string]rule{
	"padding":            {token: "spacing", applies: "container Button", raw: "length", high: 512, units: "px rem"},
	"gap":                {token: "spacing", applies: "Stack Flex Grid", raw: "length", high: 512, units: "px"},
	"marginTop":          {token: "spacing", placement: true, raw: "length", low: -256, high: 512, units: "px"},
	"marginLeft":         {token: "spacing", minimum: Free, values: "auto", raw: "length", low: -256, high: 512, units: "px"},
	"width":              {values: "auto full fit 1/2 1/3 2/3 1/4 3/4", placement: true, raw: "length", high: 4000, units: "px rem % vw"},
	"minWidth":           {raw: "length", high: 4000, units: "px % vh"},
	"minHeight":          {values: "screen", raw: "length", high: 4000, units: "px % vh"},
	"maxWidth":           {token: "container", placement: true, raw: "length", high: 4000, units: "px rem % vw"},
	"height":             {values: "auto full", raw: "length", high: 4000, units: "px % vh"},
	"aspectRatio":        {values: "1:1 4:3 3:2 16:9 21:9 3:4 9:16", applies: "Box Image Video", raw: "ratio"},
	"align":              {values: "start center end stretch between", applies: "Stack Flex Grid"},
	"justify":            {values: "start center end stretch between", applies: "Stack Flex Grid"},
	"alignSelf":          {values: "start center end stretch", applies: "parent:Stack parent:Flex parent:Grid", placement: true},
	"justifySelf":        {values: "start center end stretch", applies: "parent:Stack parent:Flex parent:Grid", placement: true},
	"columns":            {applies: "Grid", raw: "tracks"},
	"rows":               {applies: "Grid", raw: "tracks"},
	"colSpan":            {values: "full", applies: "parent:Grid", placement: true, raw: "integer", low: 1, high: 12},
	"rowSpan":            {values: "full", applies: "parent:Grid", placement: true, raw: "integer", low: 1, high: 12},
	"colStart":           {applies: "parent:Grid", minimum: Free, raw: "integer", low: 1, high: 13},
	"rowStart":           {applies: "parent:Grid", minimum: Free, raw: "integer", low: 1, high: 13},
	"order":              {applies: "parent:Stack parent:Flex parent:Grid", placement: true, raw: "integer", low: -10, high: 10},
	"background":         {token: "colors", applies: "container Button", raw: "color"},
	"backgroundSize":     {values: "cover contain", applies: "Box"},
	"backgroundPosition": {values: "center top bottom", applies: "Box"},
	"color":              {token: "colors", raw: "color"},
	"typography":         {token: "typography", applies: "Typography Button Link"},
	"textAlign":          {values: "start center end", applies: "Typography"},
	"lineClamp":          {applies: "Text Heading", raw: "integer", low: 1, high: 10},
	"fontWeight":         {minimum: Free, applies: "Typography", raw: "weight"},
	"radius":             {token: "radius", raw: "length", high: 512, units: "px"},
	"borderWidth":        {token: "borderWidth"},
	"borderColor":        {token: "colors", raw: "color"},
	"borderSides":        {values: "all top bottom x y"},
	"shadow":             {token: "shadow"},
	"opacity":            {minimum: Free, raw: "opacity", high: 1},
	"overflow":           {values: "visible hidden", applies: "container"},
	"position":           {values: "static relative"},
	"top":                {minimum: Free, token: "spacing", raw: "length", low: -2000, high: 4000, units: "px %"},
	"zIndex":             {minimum: Free, token: "layer"},
	"hidden":             {placement: true, raw: "bool"},
	"transition":         {token: "transition"},
	"outline":            {token: "colors", applies: "interactive"},
}

func init() {
	for _, key := range []string{"paddingX", "paddingY", "paddingTop", "paddingRight", "paddingBottom", "paddingLeft"} {
		catalogue[key] = catalogue["padding"]
	}
	for _, key := range []string{"rowGap", "columnGap"} {
		catalogue[key] = catalogue["gap"]
	}
	catalogue["marginBottom"] = catalogue["marginTop"]
	for _, key := range []string{"marginRight", "marginX"} {
		catalogue[key] = catalogue["marginLeft"]
	}
	for _, key := range []string{"right", "bottom", "left"} {
		catalogue[key] = catalogue["top"]
	}
}
func (c *checker) design(n map[string]any, typ, parent string, p policy, id string) {
	d := obj(n["design"])
	var check func(string, any, string, string)
	check = func(key string, v any, path, state string) {
		r, ok := catalogue[key]
		if !ok {
			c.error("DESIGN_PROPERTY_UNKNOWN", path, id, nil)
			return
		}
		if !c.applies(r.applies, typ, parent) {
			c.error("DESIGN_PROPERTY_NOT_APPLICABLE", path, id, nil)
			return
		}
		if p.mode < r.minimum || (p.mode == Strict && !r.placement) {
			c.error("DESIGN_PROPERTY_MODE_REQUIRED", path, id, nil)
			return
		}
		if key == "outline" && state != "focusVisible" {
			c.error("DESIGN_PROPERTY_NOT_APPLICABLE", path, id, nil)
			return
		}
		if slices.Contains([]string{"top", "right", "bottom", "left"}, key) {
			position := d["position"]
			valid := position == "relative" || position == "sticky" || position == "absolute"
			if m := obj(position); m != nil {
				valid = true
				for _, v := range m {
					if v != "relative" && v != "sticky" && v != "absolute" {
						valid = false
					}
				}
			}
			if !valid {
				c.error("DESIGN_POSITION_REQUIRED", path, id, nil)
				return
			}
		}
		m := obj(v)
		if m != nil {
			if _, raw := m["raw"]; !raw {
				if state != "" {
					c.error("DESIGN_STATE_RESPONSIVE_FORBIDDEN", path, id, nil)
					return
				}
				for _, bp := range keys(m) {
					if bp != "base" {
						if _, ok := obj(c.app["breakpoints"])[bp]; !ok {
							c.error("DESIGN_BREAKPOINT_UNKNOWN", path+"/"+bp, id, nil)
							continue
						}
					}
					check(key, m[bp], path+"/"+bp, state)
				}
				return
			}
		}
		if !c.value(key, r, v, p) {
			c.error("DESIGN_VALUE_INVALID", path, id, map[string]any{"property": key, "mode": p.mode.String()})
		}
	}
	for _, key := range keys(d) {
		if key == "states" {
			for _, state := range keys(obj(d[key])) {
				for _, prop := range keys(obj(obj(d[key])[state])) {
					check(prop, obj(obj(d[key])[state])[prop], ir.Pointer("nodes", id, "design", "states", state, prop), state)
				}
			}
			continue
		}
		check(key, d[key], ir.Pointer("nodes", id, "design", key), "")
	}
}
func (c *checker) applies(a, typ, parent string) bool {
	if a == "" {
		return true
	}
	for _, s := range strings.Fields(a) {
		if s == typ || (s == "Typography" && (slices.Contains([]string{"Text", "Heading", "RichText", "Label"}, typ) || obj(obj(c.app["primitives"])[typ])["group"] == "Typography")) || (s == "container" && nativeContainer(c.app, typ)) || s == "parent:"+parent {
			return true
		}
		if s == "interactive" && (typ == "Button" || typ == "Link" || typ == "Input" || typ == "Checkbox" || typ == "Select" || obj(obj(c.app["primitives"])[typ])["group"] == "Actions" || obj(obj(c.app["components"])[typ])["group"] == "Actions") {
			return true
		}
	}
	return false
}
func (c *checker) value(key string, r rule, v any, p policy) bool {
	raw := false
	if m := obj(v); m != nil {
		v = m["raw"]
		raw = true
		if p.mode < Free {
			return false
		}
	}
	if !raw {
		if s, ok := v.(string); ok {
			if slices.Contains(strings.Fields(r.values), s) {
				return true
			}
			if (key == "position" && (s == "sticky" || s == "absolute") || key == "overflow" && s == "auto") && p.mode >= Free {
				return true
			}
			if r.raw == "tracks" && c.track(s, System) {
				return true
			}
			if r.token != "" {
				if _, ok := obj(obj(c.app["tokens"])[r.token])[s]; ok {
					return true
				}
			}
		}
		switch r.raw {
		case "integer", "opacity", "weight":
			return c.numeric(r, v)
		case "bool":
			_, ok := v.(bool)
			return ok
		case "tracks":
			if n, ok := number(v); ok {
				return n >= 1 && n <= 12 && math.Trunc(n) == n
			}
			if a, ok := v.([]any); ok {
				return c.tracks(a, p)
			}
		}
		return false
	}
	switch r.raw {
	case "length":
		s, ok := v.(string)
		if !ok {
			return false
		}
		low, high := r.low, r.high
		limit := obj(obj(c.options.Project["limits"])[key])
		if n, ok := number(limit["min"]); ok {
			low = max(low, n)
		}
		if n, ok := number(limit["max"]); ok {
			high = min(high, n)
		}
		return length(s, r.units, low, high)
	case "color":
		s, ok := v.(string)
		return ok && p.rawColors && color.MatchString(s)
	case "ratio":
		s, ok := v.(string)
		if !ok || !ratio.MatchString(s) {
			return false
		}
		parts := strings.Split(s, ":")
		a, _ := strconv.Atoi(parts[0])
		b, _ := strconv.Atoi(parts[1])
		return a >= 1 && a <= 32 && b >= 1 && b <= 32
	case "tracks":
		s, ok := v.(string)
		return ok && c.track(s, Free)
	}
	return false
}
func (c *checker) numeric(r rule, v any) bool {
	n, ok := number(v)
	if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
		return false
	}
	switch r.raw {
	case "integer":
		return n >= r.low && n <= r.high && math.Trunc(n) == n
	case "opacity":
		return n >= 0 && n <= 1 && math.Abs(n*20-math.Round(n*20)) < 1e-9
	case "weight":
		for _, v := range obj(obj(c.app["tokens"])["typography"]) {
			if w, ok := number(obj(v)["fontWeight"]); ok && n == w {
				return true
			}
		}
	}
	return false
}

var dimensions = regexp.MustCompile(`^(-?[0-9]+(?:\.[0-9]{1,4})?)(px|rem|%|vw|vh)$`)
var color = regexp.MustCompile(`^#[0-9a-fA-F]{6}(?:[0-9a-fA-F]{2})?$`)
var ratio = regexp.MustCompile(`^[0-9]{1,2}:[0-9]{1,2}$`)
var fraction = regexp.MustCompile(`^([0-9]{1,2})fr$`)
var minimumMaximum = regexp.MustCompile(`^minmax\(([^,()]+),\s*([^,()]+)\)$`)
var repeat = regexp.MustCompile(`^repeat\(auto-fill,\s*(minmax\([^()]+\))\)$`)

func length(s, units string, low, high float64) bool {
	parts := dimensions.FindStringSubmatch(s)
	if parts == nil || !slices.Contains(strings.Fields(units), parts[2]) {
		return false
	}
	n, e := strconv.ParseFloat(parts[1], 64)
	if e != nil {
		return false
	}
	if parts[2] == "rem" {
		n *= 16
	}
	if parts[2] == "%" || parts[2] == "vw" || parts[2] == "vh" {
		if low < 0 {
			return n >= max(low, -100) && n <= min(high, 100)
		}
		return n >= max(low, 0) && n <= min(high, 100)
	}
	return n >= low && n <= high
}
func (c *checker) tracks(a []any, p policy) bool {
	if len(a) == 0 || len(a) > 12 {
		return false
	}
	for _, v := range a {
		if m := obj(v); m != nil {
			s, ok := m["raw"].(string)
			if !ok || p.mode < Free || !c.track(s, Free) {
				return false
			}
		} else {
			s, ok := v.(string)
			if !ok || !c.track(s, System) {
				return false
			}
		}
	}
	return true
}
func (c *checker) track(s string, m Mode) bool {
	if s == "auto" {
		return true
	}
	if p := fraction.FindStringSubmatch(s); p != nil {
		n, _ := strconv.Atoi(p[1])
		return n >= 1 && n <= 12
	}
	if m < Free {
		return false
	}
	if length(s, "px %", 0, 4000) {
		return true
	}
	if p := minimumMaximum.FindStringSubmatch(s); p != nil {
		return length(p[1], "px %", 0, 4000) && (length(p[2], "px %", 0, 4000) || fraction.MatchString(p[2]) && c.track(p[2], System))
	}
	if p := repeat.FindStringSubmatch(s); p != nil {
		return c.track(p[1], Free)
	}
	return false
}
