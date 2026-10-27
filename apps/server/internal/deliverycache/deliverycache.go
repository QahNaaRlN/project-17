// Package deliverycache — кэш published-ответов Delivery API на origin (07 §4,
// docs/delivery-cache-limits.md, STU-523…529).
//
// Запись `dlv:{project}:{env}:{kind}:{key}:{locale}` хранит тело, ETag, Surrogate-Key и
// версии тегов `tagv:{surrogate key}`, прочитанные до чтения PostgreSQL. Публикация меняет
// версии тегов (задача outbox), и попадание с устаревшими версиями считается промахом: ответ,
// прочитанный до публикации и записанный после инвалидации, не будет отдан.
//
// Каждая публикация, откат и promote меняют тег `routes` окружения (publishing.enqueuePurge),
// поэтому для /page достаточно заранее прочитать версию `routes`: остальные теги страницы
// становятся известны только после чтения БД.
package deliverycache

import (
	"context"
	"encoding/json"
	"net/url"
	"time"

	"github.com/google/uuid"

	"github.com/qahnaarln/project-17/apps/server/internal/redisstore"
)

const (
	// TTL записи (07 §4): 1 ч плюс явная инвалидация.
	TTL = time.Hour
	// tagTTL больше TTL записи: тег не истекает раньше записей, которые на него ссылаются.
	tagTTL = 2 * TTL
	// MaxEntry — больший ответ отдаётся без кэша (STU-529).
	MaxEntry = 1 << 20
)

// Entry — сохранённый ответ.
type Entry struct {
	Body          []byte            `json:"b"`
	ETag          string            `json:"e"`
	SurrogateKeys []string          `json:"k"`
	Tags          map[string]string `json:"t"`
}

// Cache — кэш поверх Redis; nil — кэша нет.
type Cache struct{ redis *redisstore.Client }

// New — кэш; без клиента Redis возвращает nil.
func New(c *redisstore.Client) *Cache {
	if c == nil {
		return nil
	}
	return &Cache{redis: c}
}

// Key — ключ записи; путь и локаль экранируются, чтобы «:» в них не склеивал разные ключи.
func Key(project, env, kind, key, locale string) string {
	return "dlv:" + project + ":" + env + ":" + kind + ":" + url.QueryEscape(key) + ":" + url.QueryEscape(locale)
}

func tagKey(surrogate string) string { return "tagv:" + surrogate }

// Get возвращает запись, если она есть и её теги не менялись.
func (c *Cache) Get(ctx context.Context, key string) *Entry {
	raw, err := c.redis.R.Get(ctx, key).Bytes()
	if !c.redis.Observe(ctx, err) || err != nil {
		return nil
	}
	var e Entry
	if json.Unmarshal(raw, &e) != nil {
		return nil
	}
	current, ok := c.versions(ctx, e.SurrogateKeys)
	if !ok {
		return nil
	}
	for _, k := range e.SurrogateKeys {
		if current[k] != e.Tags[k] {
			return nil
		}
	}
	return &e
}

// Versions читает версии тегов до чтения БД; false — Redis недоступен, ответ не сохранять.
func (c *Cache) Versions(ctx context.Context, surrogateKeys []string) (map[string]string, bool) {
	return c.versions(ctx, surrogateKeys)
}

func (c *Cache) versions(ctx context.Context, surrogateKeys []string) (map[string]string, bool) {
	out := map[string]string{}
	if len(surrogateKeys) == 0 {
		return out, true
	}
	keys := make([]string, len(surrogateKeys))
	for i, k := range surrogateKeys {
		keys[i] = tagKey(k)
	}
	values, err := c.redis.R.MGet(ctx, keys...).Result()
	if !c.redis.Observe(ctx, err) {
		return nil, false
	}
	for i, v := range values {
		s, _ := v.(string)
		out[surrogateKeys[i]] = s
	}
	return out, true
}

// Put сохраняет ответ. Версии тегов, не прочитанных заранее (pre), читаются сейчас.
// Возвращает false, если ответ не сохранён (велик или Redis недоступен).
func (c *Cache) Put(ctx context.Context, key string, e Entry, pre map[string]string) bool {
	if len(e.Body) > MaxEntry {
		return false
	}
	var rest []string
	for _, k := range e.SurrogateKeys {
		if _, ok := pre[k]; !ok {
			rest = append(rest, k)
		}
	}
	later, ok := c.versions(ctx, rest)
	if !ok {
		return false
	}
	e.Tags = map[string]string{}
	for _, k := range e.SurrogateKeys {
		if v, ok := pre[k]; ok {
			e.Tags[k] = v
		} else {
			e.Tags[k] = later[k]
		}
	}
	raw, _ := json.Marshal(e)
	return c.redis.Observe(ctx, c.redis.R.Set(ctx, key, raw, TTL).Err())
}

// Invalidate меняет версии тегов: записи, зависящие от них, перестают отдаваться.
// Новая версия уникальна, поэтому вытесненный и заново созданный тег не совпадёт со старым.
func (c *Cache) Invalidate(ctx context.Context, surrogateKeys []string) error {
	if c == nil || len(surrogateKeys) == 0 {
		return nil
	}
	pipe := c.redis.R.Pipeline()
	for _, k := range surrogateKeys {
		pipe.Set(ctx, tagKey(k), uuid.Must(uuid.NewV7()).String(), tagTTL)
	}
	_, err := pipe.Exec(ctx)
	c.redis.Observe(ctx, err)
	return err
}
