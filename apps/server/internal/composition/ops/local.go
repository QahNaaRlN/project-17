package ops

import (
	"bytes"
	"encoding/json"
	"regexp"

	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
)

var localKey = regexp.MustCompile(`^t_[a-z0-9_]{2,40}$`)
var localLocale = regexp.MustCompile(`^[a-z]{2,3}(-[A-Z]{2})?$`)

type localPayload struct {
	Key    string          `json:"key"`
	Type   string          `json:"type"`
	Locale string          `json:"locale,omitempty"`
	Value  json.RawMessage `json:"value"`
}

// Locale writes merge translations; full maps are accepted for exact inverses.
// This slice supports plain text only; other content types remain explicit errors.
func localContentSet(doc map[string]any, raw json.RawMessage, _ ir.IDGenerator) (Result, error) {
	var p localPayload
	if err := decode(raw, &p); err != nil {
		return Result{}, err
	}
	if !localKey.MatchString(p.Key) || p.Type != "text" {
		return Result{}, fail("PAYLOAD_INVALID", "нужен key t_… и type=text")
	}
	values, _ := doc["localContent"].(map[string]any)
	if values == nil {
		values = map[string]any{}
	}
	before := values[p.Key]
	previous := map[string]any{}
	if before != nil {
		item, _ := before.(map[string]any)
		if item["type"] != "text" {
			return Result{}, fail("PAYLOAD_INVALID", "нельзя заменить другой тип локального контента")
		}
		original, ok := item["value"].(map[string]any)
		if !ok {
			return Result{}, fail("PAYLOAD_INVALID", "нужна карта переводов")
		}
		for locale, value := range original {
			previous[locale] = value
		}
	}
	if p.Locale != "" {
		var value string
		if !localLocale.MatchString(p.Locale) || len(p.Value) == 0 || bytes.Equal(bytes.TrimSpace(p.Value), []byte("null")) || json.Unmarshal(p.Value, &value) != nil {
			return Result{}, fail("PAYLOAD_INVALID", "нужна локаль и строковое значение")
		}
		previous[p.Locale] = value
	} else {
		var translations map[string]json.RawMessage
		if json.Unmarshal(p.Value, &translations) != nil || len(translations) == 0 {
			return Result{}, fail("PAYLOAD_INVALID", "нужна непустая карта переводов")
		}
		previous = map[string]any{}
		for locale, raw := range translations {
			var value string
			if !localLocale.MatchString(locale) || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &value) != nil {
				return Result{}, fail("PAYLOAD_INVALID", "некорректная локаль")
			}
			previous[locale] = value
		}
	}
	inverse := Op{Type: LocalContentRemove, Payload: mustRaw(map[string]any{"key": p.Key})}
	if before != nil {
		inverse = Op{Type: LocalContentSet, Payload: mustRaw(map[string]any{"key": p.Key, "type": "text", "value": before.(map[string]any)["value"]})}
	}
	after := map[string]any{"type": "text", "value": previous}
	values[p.Key] = after
	doc["localContent"] = values
	return Result{Before: before, After: after, Inverse: inverse}, nil
}
func localContentRemove(doc map[string]any, raw json.RawMessage, _ ir.IDGenerator) (Result, error) {
	var p struct {
		Key string `json:"key"`
	}
	if err := decode(raw, &p); err != nil {
		return Result{}, err
	}
	if !localKey.MatchString(p.Key) {
		return Result{}, fail("PAYLOAD_INVALID", "некорректный key")
	}
	values, _ := doc["localContent"].(map[string]any)
	item, ok := values[p.Key].(map[string]any)
	if !ok {
		return Result{}, fail("LOCAL_CONTENT_NOT_FOUND", "локальный контент не найден")
	}
	if item["type"] != "text" {
		return Result{}, fail("PAYLOAD_INVALID", "пока поддержан только text")
	}
	if _, ok := item["value"].(map[string]any); !ok {
		return Result{}, fail("PAYLOAD_INVALID", "нужна карта переводов")
	}
	delete(values, p.Key)
	if len(values) == 0 {
		delete(doc, "localContent")
	}
	return Result{Before: item, Inverse: Op{Type: LocalContentSet, Payload: mustRaw(map[string]any{"key": p.Key, "type": "text", "value": item["value"]})}}, nil
}
