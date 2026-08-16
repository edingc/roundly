// Package httpx holds the JSON request/response conventions shared by every
// domain handler: one error envelope, one decode path, one validation type.
package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
)

// ErrorBody is the single error shape the API returns. `fields` is populated
// only for validation failures so the frontend can attach messages to inputs.
type ErrorBody struct {
	Error   string            `json:"error"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

// APIError is an error carrying an HTTP status and a stable machine-readable code.
type APIError struct {
	Status  int
	Code    string
	Message string
	// Fields is per-field validation detail, and nothing else. The client keys
	// off its presence to decide whether to attach messages to inputs, so an
	// error that puts unrelated data here renders as a form full of errors
	// against fields that do not exist.
	Fields map[string]string
	// Headers are written alongside the response. For the handful of errors
	// that have a standard header to carry their detail — Retry-After on a 429
	// — which is where a client or a proxy actually looks for it.
	Headers map[string]string
	err     error
}

func (e *APIError) Error() string {
	if e.err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.err)
	}
	return e.Message
}

func (e *APIError) Unwrap() error { return e.err }

// Wrap attaches an underlying cause that is logged but never sent to the client.
func (e *APIError) Wrap(err error) *APIError {
	clone := *e
	clone.err = err
	return &clone
}

func newAPIError(status int, code, message string) *APIError {
	return &APIError{Status: status, Code: code, Message: message}
}

func BadRequest(message string) *APIError {
	return newAPIError(http.StatusBadRequest, "bad_request", message)
}

func Unauthorized(message string) *APIError {
	return newAPIError(http.StatusUnauthorized, "unauthorized", message)
}

func Forbidden(message string) *APIError {
	return newAPIError(http.StatusForbidden, "forbidden", message)
}

func NotFound(message string) *APIError {
	return newAPIError(http.StatusNotFound, "not_found", message)
}

func Conflict(message string) *APIError {
	return newAPIError(http.StatusConflict, "conflict", message)
}

// TooManyRequests reports that a caller has exceeded its rate limit. Callers
// are expected to set Retry-After alongside it, since the status alone does not
// tell the client when to come back.
func TooManyRequests(message string) *APIError {
	return newAPIError(http.StatusTooManyRequests, "rate_limited", message)
}

func Internal(err error) *APIError {
	return (&APIError{
		Status:  http.StatusInternalServerError,
		Code:    "internal_error",
		Message: "Something went wrong. Please try again.",
	}).Wrap(err)
}

// ValidationError reports per-field validation problems.
func ValidationError(fields map[string]string) *APIError {
	return &APIError{
		Status:  http.StatusUnprocessableEntity,
		Code:    "validation_failed",
		Message: "Please correct the highlighted fields.",
		Fields:  fields,
	}
}

// JSON writes v as JSON with the given status.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if v == nil || status == http.StatusNoContent {
		w.WriteHeader(status)
		return
	}
	buf, err := json.Marshal(v)
	if err != nil {
		slog.Error("marshal response", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal_error","message":"Something went wrong. Please try again."}`))
		return
	}
	w.WriteHeader(status)
	_, _ = w.Write(buf)
}

// NoContent replies 204.
func NoContent(w http.ResponseWriter) { JSON(w, http.StatusNoContent, nil) }

// Error writes err as a JSON error envelope, logging 5xx causes server-side.
func Error(w http.ResponseWriter, r *http.Request, err error) {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		apiErr = Internal(err)
	}

	if apiErr.Status >= http.StatusInternalServerError {
		slog.Error("request failed",
			"method", r.Method,
			"path", r.URL.Path,
			"status", apiErr.Status,
			"error", apiErr.Error(),
		)
	}

	for name, value := range apiErr.Headers {
		w.Header().Set(name, value)
	}
	JSON(w, apiErr.Status, ErrorBody{
		Error:   apiErr.Code,
		Message: apiErr.Message,
		Fields:  apiErr.Fields,
	})
}

// maxBodyBytes caps request bodies; the largest legitimate payload is a course
// with 18 holes of tee detail, which is well under this.
const maxBodyBytes = 1 << 20 // 1 MiB

// Decode reads a JSON body into dst, rejecting unknown fields so typos in
// client payloads surface loudly instead of being silently dropped.
func Decode(w http.ResponseWriter, r *http.Request, dst any) error {
	return DecodeLimit(w, r, dst, maxBodyBytes)
}

// DecodeLimit is Decode with a caller-chosen size cap, for the endpoints whose
// legitimate payload is larger than a single course — an account restore
// carries every course and club the user owns.
func DecodeLimit(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) error {
	if ct := r.Header.Get("Content-Type"); ct != "" {
		if mediaType := strings.TrimSpace(strings.Split(ct, ";")[0]); mediaType != "application/json" {
			return BadRequest("Content-Type must be application/json.")
		}
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		var syntaxErr *json.SyntaxError
		var typeErr *json.UnmarshalTypeError
		var maxErr *http.MaxBytesError

		switch {
		case errors.As(err, &syntaxErr):
			return BadRequest(fmt.Sprintf("Malformed JSON at position %d.", syntaxErr.Offset))
		case errors.As(err, &typeErr):
			if typeErr.Field != "" {
				return BadRequest(fmt.Sprintf("Field %q has the wrong type.", typeErr.Field))
			}
			return BadRequest("Request body has the wrong type.")
		case errors.Is(err, io.EOF):
			return BadRequest("Request body must not be empty.")
		case errors.As(err, &maxErr):
			return BadRequest("Request body is too large.")
		case strings.HasPrefix(err.Error(), "json: unknown field "):
			field := strings.TrimPrefix(err.Error(), "json: unknown field ")
			return BadRequest(fmt.Sprintf("Unknown field %s.", field))
		default:
			return BadRequest("Could not parse request body.")
		}
	}

	// A second value means the client sent concatenated JSON documents.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return BadRequest("Request body must contain a single JSON object.")
	}
	return nil
}

// QueryInt reads an integer query parameter, clamped to [min, max].
func QueryInt(r *http.Request, key string, def, min, max int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}
