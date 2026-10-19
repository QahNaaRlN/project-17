// Package previeworigins holds the operator list of preview application origins
// (STU-328). Only exact origins are accepted: wildcard patterns are rejected at
// startup so a registration can never widen what Studio embeds.
package previeworigins

import (
	"errors"
	"net"
	"net/url"
	"slices"
	"strings"
)

// ErrConfig — CMS_PREVIEW_APP_ORIGINS is malformed.
var ErrConfig = errors.New("CMS_PREVIEW_APP_ORIGINS: список точных origin https:// (http:// только для loopback) через запятую")

// List — разрешённые origin preview-приложений.
type List struct{ origins []string }

// Parse reads a comma-separated list. Empty input yields an empty list.
func Parse(raw string) (List, error) {
	var out []string
	for part := range strings.SplitSeq(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		origin, ok := canonical(part, true)
		if !ok {
			return List{}, ErrConfig
		}
		if !slices.Contains(out, origin) {
			out = append(out, origin)
		}
	}
	slices.Sort(out)
	return List{origins: out}, nil
}

// Origins returns the sorted list (for Studio CSP frame-src).
func (l List) Origins() []string { return slices.Clone(l.origins) }

// AppURL validates a registration URL and returns it normalised. The URL must be
// absolute, without userinfo, query or fragment, and its origin must be listed.
func (l List) AppURL(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#") || len(raw) > 2048 {
		return "", false
	}
	origin, ok := canonical(u.Scheme+"://"+u.Host, false)
	if !ok || !slices.Contains(l.origins, origin) {
		return "", false
	}
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	return origin + path, true
}

func canonical(raw string, bare bool) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Host == "" || u.Opaque != "" || strings.ContainsAny(u.Host, "*%") {
		return "", false
	}
	if bare && (u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#")) {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	switch u.Scheme {
	case "https":
	case "http":
		if host != "localhost" && !loopback(host) {
			return "", false
		}
	default:
		return "", false
	}
	h := u.Host
	if port := u.Port(); port != "" {
		h = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		h = "[" + host + "]"
	} else {
		h = host
	}
	return u.Scheme + "://" + h, true
}

func loopback(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
