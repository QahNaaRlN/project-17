package commandbus

import (
	"fmt"
	"net/http"
)

// Error — ошибка команды с HTTP-статусом и кодом из каталога (08-api.md §6).
// HTTP-слой отдаёт её как application/problem+json.
type Error struct {
	Status int
	Code   string
	Title  string
	Detail string
	Params map[string]any
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Detail) }

// NewError создаёт ошибку команды.
func NewError(status int, code, title, detail string) *Error {
	return &Error{Status: status, Code: code, Title: title, Detail: detail}
}

// WithParams добавляет параметры ошибки.
func (e *Error) WithParams(params map[string]any) *Error {
	e.Params = params
	return e
}

// Validation — ошибка проверки payload (422 VALIDATION_FAILED); fields — поле → описание.
func Validation(fields map[string]string) *Error {
	params := make(map[string]any, len(fields))
	for k, v := range fields {
		params[k] = v
	}
	return &Error{
		Status: http.StatusUnprocessableEntity,
		Code:   "VALIDATION_FAILED",
		Title:  "Данные команды не прошли проверку",
		Detail: "Исправьте поля, перечисленные в params",
		Params: map[string]any{"fields": params},
	}
}
