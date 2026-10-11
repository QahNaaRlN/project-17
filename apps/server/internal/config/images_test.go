package config

import (
	"strings"
	"testing"
)

func TestImagesConfig(t *testing.T) {
	values := map[string]string{"CMS_DATABASE_URL": "postgres://x", "CMS_S3_ENDPOINT": "localhost:9000", "CMS_S3_ACCESS_KEY": "a", "CMS_S3_SECRET_KEY": "b", "CMS_S3_BUCKET": "assets", "CMS_ASSET_PUBLIC_URL": "http://localhost:8080", "CMS_ASSET_SIGNING_KEY": strings.Repeat("11", 32), "CMS_IMGPROXY_URL": "http://imgproxy:8080", "CMS_IMGPROXY_KEY": strings.Repeat("22", 32), "CMS_IMGPROXY_SALT": strings.Repeat("33", 16)}
	c, e := FromEnv(env(values))
	if e != nil || c.Images.PublicURL != "http://localhost:8080" || c.Images.Bucket != "assets" {
		t.Fatal(c, e)
	}
	delete(values, "CMS_IMGPROXY_SALT")
	if _, e := FromEnv(env(values)); e == nil {
		t.Fatal("partial image configuration")
	}
}
