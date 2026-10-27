package config

import (
	"strings"
	"testing"

	"github.com/qahnaarln/project-17/apps/server/internal/ratelimit"
)

// STU-520, 531: Redis is optional; limits default to 08 §7 and accept 0 to switch off.
func TestRedisAndRateLimits(t *testing.T) {
	cfg, err := FromEnv(env(map[string]string{"CMS_DATABASE_URL": "postgres://x"}))
	if err != nil || cfg.RedisURL != "" || cfg.RateLimits != ratelimit.Defaults() {
		t.Fatal(cfg.RedisURL, cfg.RateLimits, err)
	}
	cfg, err = FromEnv(env(map[string]string{"CMS_DATABASE_URL": "postgres://x", "CMS_REDIS_URL": "redis://redis:6379/0",
		"CMS_RATE_DELIVERY": "0", "CMS_RATE_AGENT_COMMANDS": "2"}))
	if err != nil || cfg.RedisURL != "redis://redis:6379/0" || cfg.RateLimits.Delivery != 0 || cfg.RateLimits.AgentCommands != 2 || cfg.RateLimits.HumanRequests != 200 {
		t.Fatal(cfg.RateLimits, err)
	}
	_, err = FromEnv(env(map[string]string{"CMS_DATABASE_URL": "postgres://x", "CMS_REDIS_URL": "http://nope", "CMS_RATE_PREVIEW": "-1", "CMS_RATE_HUMAN_COMMANDS": "x"}))
	for _, want := range []string{"CMS_REDIS_URL", "CMS_RATE_PREVIEW", "CMS_RATE_HUMAN_COMMANDS"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatal(want, err)
		}
	}
}
