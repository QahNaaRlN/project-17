// Package imagedelivery gates image transformations by content version and signed capabilities.
package imagedelivery

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var Widths = []int{320, 480, 640, 768, 1024, 1280, 1600, 1920, 2560}

const TTL = 15 * time.Minute
const MaxBody = 32 << 20

type Config struct{ PublicURL, ProxyURL, Bucket, Key, ProxyKey, ProxySalt string }
type Options struct {
	Width, Height int
	Fit, Format   string
}

func invalid(detail string) error { return fmt.Errorf("image delivery: %s", detail) }
func (o Options) Validate() error {
	found := false
	for _, w := range Widths {
		found = found || w == o.Width
	}
	if !found || o.Height < 0 || o.Height > 2560 || (o.Fit != "contain" && o.Fit != "cover") || (o.Fit == "contain" && o.Height != 0) || (o.Fit == "cover" && o.Height == 0) {
		return invalid("unsupported dimensions or fit")
	}
	switch o.Format {
	case "avif", "webp", "jpeg", "png":
		return nil
	}
	return invalid("unsupported format")
}

// ParseOptions rejects unknown/duplicate parameters rather than letting caches interpret them differently.
func ParseOptions(q url.Values) (Options, error) {
	o := Options{Width: 640, Fit: "contain", Format: "webp"}
	for k, v := range q {
		if len(v) != 1 || v[0] == "" {
			return o, invalid("duplicate or empty option")
		}
		switch k {
		case "w", "h":
			n, e := strconv.Atoi(v[0])
			if e != nil || strconv.Itoa(n) != v[0] {
				return o, invalid("integer expected")
			}
			if k == "w" {
				o.Width = n
			} else {
				o.Height = n
			}
		case "fit":
			o.Fit = v[0]
		case "fmt":
			o.Format = v[0]
		case "changesetId": // already checked by preview authentication
		default:
			return o, invalid("unknown option")
		}
	}
	return o, o.Validate()
}
func digest(key, salt []byte, path string) string {
	m := hmac.New(sha256.New, key)
	m.Write(salt)
	m.Write([]byte(path))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
func fingerprint(raw []byte) string { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }
func secret(raw string, minimum int) ([]byte, error) {
	b, e := hex.DecodeString(raw)
	if e != nil || len(b) < minimum {
		return nil, invalid("invalid hex signing key/salt")
	}
	return b, nil
}
func base(raw string, originOnly bool) (*url.URL, error) {
	u, e := url.Parse(raw)
	if e != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (originOnly && u.Path != "" && u.Path != "/") {
		return nil, invalid("invalid base URL")
	}
	u.Path = strings.TrimSuffix(u.Path, "/")
	return u, nil
}

// ProxyPath implements imgproxy's salted HMAC-SHA256 protocol. Source is generated from verified DB state.
func (s *Service) ProxyPath(key string, o Options, x, y float64) string {
	resize := "fit"
	if o.Fit == "cover" {
		resize = "fill"
	}
	path := fmt.Sprintf("/rs:%s:%d:%d:1/g:fp:%s:%s/%s.%s", resize, o.Width, o.Height, strconv.FormatFloat(x, 'f', -1, 64), strconv.FormatFloat(y, 'f', -1, 64), base64.RawURLEncoding.EncodeToString([]byte("s3://"+s.cfg.Bucket+"/"+key)), o.Format)
	return "/" + digest(s.proxyKey, s.proxySalt, path) + path
}
