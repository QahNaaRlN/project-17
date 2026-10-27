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
	"encoding/binary"
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
	// MaxEntry — больший ответ отдаётся без кэша (STU-529). 4 МиБ вмещают таблицу маршрутов
	// профиля 01 §5 (10 000 страниц, ~1,1 МБ): с пределом 1 МиБ /routes всегда шёл в БД
	// (нагрузочные тесты).
	MaxEntry = 4 << 20
)

// Entry — сохранённый ответ.
type Entry struct {
	Body          []byte
	ETag          string
	SurrogateKeys []string
	Tags          map[string]string
}

// format — первый байт записи. Запись двоичная: JSON с телом в base64 занимал ~40 % CPU
// origin на попаданиях под нагрузкой (docs/load-test-report.md). Записи другого формата
// (в том числе прежние JSON) читаются как промах.
const format = 1

// encode: format, ETag, число ключей, пары «ключ, версия тега» — строки с длиной uvarint,
// затем тело до конца записи.
func encode(e Entry) []byte {
	n := 1 + binary.MaxVarintLen64*(2+2*len(e.SurrogateKeys)) + len(e.ETag) + len(e.Body)
	for _, k := range e.SurrogateKeys {
		n += len(k) + len(e.Tags[k])
	}
	b := append(make([]byte, 0, n), format)
	str := func(s string) { b = append(binary.AppendUvarint(b, uint64(len(s))), s...) }
	str(e.ETag)
	b = binary.AppendUvarint(b, uint64(len(e.SurrogateKeys)))
	for _, k := range e.SurrogateKeys {
		str(k)
		str(e.Tags[k])
	}
	return append(b, e.Body...)
}

// decode — обратное encode; false — запись другого формата или повреждена.
func decode(raw []byte) (Entry, bool) {
	if len(raw) == 0 || raw[0] != format {
		return Entry{}, false
	}
	raw = raw[1:]
	ok := true
	num := func() uint64 {
		v, n := binary.Uvarint(raw)
		if n <= 0 || v > uint64(len(raw)) {
			ok = false
			return 0
		}
		raw = raw[n:]
		return v
	}
	str := func() string {
		n := num()
		if !ok || n > uint64(len(raw)) {
			ok = false
			return ""
		}
		s := string(raw[:n])
		raw = raw[n:]
		return s
	}
	e := Entry{ETag: str()}
	count := num()
	if !ok {
		return Entry{}, false
	}
	e.SurrogateKeys = make([]string, 0, count)
	e.Tags = make(map[string]string, count)
	for range count {
		k, v := str(), str()
		if !ok {
			return Entry{}, false
		}
		e.SurrogateKeys = append(e.SurrogateKeys, k)
		e.Tags[k] = v
	}
	e.Body = raw
	return e, true
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
	e, ok := decode(raw)
	if !ok {
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
	return c.redis.Observe(ctx, c.redis.R.Set(ctx, key, encode(e), TTL).Err())
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
