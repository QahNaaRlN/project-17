package assets

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strconv"
	"strings"
)

// SVG is reconstructed from a small static drawing allowlist. CSS, animation,
// entity declarations and namespace tricks never reach the stored representation.
func sanitizeSVG(raw []byte) ([]byte, error) {
	d := xml.NewDecoder(bytes.NewReader(raw))
	var out bytes.Buffer
	e := xml.NewEncoder(&out)
	elements := strings.Fields("svg g defs title desc path rect circle ellipse line polyline polygon text tspan use symbol clipPath mask linearGradient radialGradient stop")
	attrs := strings.Fields("id viewBox width height x y x1 y1 x2 y2 cx cy r rx ry d points fill fill-rule fill-opacity stroke stroke-width stroke-linecap stroke-linejoin stroke-opacity opacity transform clip-path mask offset stop-color stop-opacity gradientUnits gradientTransform spreadMethod font-size font-family text-anchor dominant-baseline dx dy href")
	allowed := func(list []string, v string) bool {
		for _, s := range list {
			if s == v {
				return true
			}
		}
		return false
	}
	depth, skip, n := 0, 0, 0
	root := false
	closed := false
	for {
		t, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		n++
		if n > 200000 {
			return nil, errors.New("SVG token limit")
		}
		switch v := t.(type) {
		case xml.Directive:
			return nil, errors.New("SVG directives forbidden")
		case xml.StartElement:
			depth++
			if depth > 128 {
				return nil, errors.New("SVG depth limit")
			}
			if depth == 1 {
				if root || closed || v.Name.Local != "svg" || (v.Name.Space != "" && v.Name.Space != "http://www.w3.org/2000/svg") {
					return nil, errors.New("SVG root")
				}
				root = true
			}
			if skip > 0 {
				skip++
				continue
			}
			if !allowed(elements, v.Name.Local) || (v.Name.Space != "" && v.Name.Space != "http://www.w3.org/2000/svg") {
				skip = 1
				continue
			}
			clean := xml.StartElement{Name: xml.Name{Local: v.Name.Local}}
			if depth == 1 {
				clean.Attr = append(clean.Attr, xml.Attr{Name: xml.Name{Local: "xmlns"}, Value: "http://www.w3.org/2000/svg"})
			}
			seen := map[string]bool{}
			for _, a := range v.Attr {
				if a.Name.Space != "" || !allowed(attrs, a.Name.Local) {
					continue
				}
				if seen[a.Name.Local] {
					return nil, errors.New("SVG duplicate attribute")
				}
				seen[a.Name.Local] = true
				value := strings.TrimSpace(a.Value)
				lower := strings.ToLower(value)
				if strings.ContainsAny(value, "\\\x00") || strings.Contains(lower, "javascript:") || strings.Contains(lower, "data:") || strings.Contains(lower, "http:") || strings.Contains(lower, "https:") || strings.Contains(value, "//") {
					continue
				}
				if a.Name.Local == "href" && (!strings.HasPrefix(value, "#") || len(value) < 2) {
					continue
				}
				if strings.Contains(lower, "url(") {
					if !strings.HasPrefix(value, "url(#") || !strings.HasSuffix(value, ")") || strings.ContainsAny(value[5:len(value)-1], " ()\t\r\n'") {
						continue
					}
				}
				clean.Attr = append(clean.Attr, xml.Attr{Name: xml.Name{Local: a.Name.Local}, Value: value})
			}
			if err := e.EncodeToken(clean); err != nil {
				return nil, err
			}
		case xml.EndElement:
			if skip > 0 {
				skip--
				depth--
				continue
			}
			if err := e.EncodeToken(xml.EndElement{Name: xml.Name{Local: v.Name.Local}}); err != nil {
				return nil, err
			}
			depth--
			if depth == 0 {
				closed = true
			}
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(v)) != "" {
				return nil, errors.New("SVG trailing text")
			}
			if skip == 0 && depth > 0 {
				if err := e.EncodeToken(v); err != nil {
					return nil, err
				}
			}
		}
	}
	if !root || !closed || depth != 0 {
		return nil, errors.New("SVG incomplete")
	}
	if err := e.Flush(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
func svgDimensions(raw []byte) (int, int) {
	d := xml.NewDecoder(bytes.NewReader(raw))
	for {
		t, err := d.Token()
		if err != nil {
			return 0, 0
		}
		if start, ok := t.(xml.StartElement); ok {
			w, h := 0, 0
			for _, a := range start.Attr {
				v, _ := strconv.ParseFloat(strings.TrimSuffix(a.Value, "px"), 64)
				if v > 0 && v <= 40000000 {
					if a.Name.Local == "width" {
						w = int(v)
					}
					if a.Name.Local == "height" {
						h = int(v)
					}
				}
			}
			return w, h
		}
	}
}
