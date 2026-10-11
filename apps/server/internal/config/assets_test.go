package config

import "testing"

func TestS3Config(t *testing.T) {
	values := map[string]string{"CMS_DATABASE_URL": "postgres://x", "CMS_S3_ENDPOINT": "localhost:9000", "CMS_S3_ACCESS_KEY": "a", "CMS_S3_SECRET_KEY": "b", "CMS_S3_BUCKET": "assets", "CMS_S3_SECURE": "false"}
	c, err := FromEnv(env(values))
	if err != nil || c.S3Secure || c.S3Endpoint != "localhost:9000" {
		t.Fatal(c, err)
	}
	values["CMS_S3_SECURE"] = "bad"
	if _, err := FromEnv(env(values)); err == nil {
		t.Fatal("invalid TLS flag")
	}
	delete(values, "CMS_S3_BUCKET")
	if _, err := FromEnv(env(values)); err == nil {
		t.Fatal("partial configuration")
	}
}
