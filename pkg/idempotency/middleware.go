package idempotency

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

type ScopeFunc func(c echo.Context) string

type record struct {
	fingerprint string
	inFlight    bool
	expiresAt   time.Time
	status      int
	contentType string
	body        []byte
}

type Store struct {
	ttl            time.Duration
	scope          ScopeFunc
	cleanupEvery   uint64
	requestCounter uint64
	records        map[string]record
	mu             sync.Mutex
	now            func() time.Time
}

func NewStore(ttl time.Duration, scope ScopeFunc) *Store {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	if scope == nil {
		scope = ClientIPScope
	}

	return &Store{
		ttl:          ttl,
		scope:        scope,
		cleanupEvery: 128,
		records:      make(map[string]record),
		now:          time.Now,
	}
}

func ClientIPScope(c echo.Context) string {
	ip := strings.TrimSpace(c.RealIP())
	if ip == "" {
		return "ip:unknown"
	}
	return "ip:" + ip
}

func AccountOrIPScope(accountIDFromContext func(context.Context) (uint, bool)) ScopeFunc {
	return func(c echo.Context) string {
		if accountIDFromContext != nil {
			if accountID, ok := accountIDFromContext(c.Request().Context()); ok && accountID > 0 {
				return "account:" + strconv.FormatUint(uint64(accountID), 10)
			}
		}
		return ClientIPScope(c)
	}
}

func (s *Store) Middleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			key := strings.TrimSpace(c.Request().Header.Get("Idempotency-Key"))
			if key == "" {
				return next(c)
			}
			if len(key) > 128 {
				return echo.NewHTTPError(http.StatusBadRequest, "idempotency key must be 128 characters or less")
			}

			rawBody, err := readAndRestoreBody(c)
			if err != nil {
				return echo.NewHTTPError(http.StatusBadRequest, "failed to read request body")
			}

			fingerprint := makeFingerprint(c.Request().Method, c.Path(), rawBody)
			recordKey := s.makeRecordKey(c, key)

			replayRecord, stateErr := s.beginOrReplay(recordKey, fingerprint)
			if stateErr != nil {
				switch {
				case errors.Is(stateErr, errIdempotencyConflict):
					return echo.NewHTTPError(http.StatusConflict, "idempotency key reuse with different request payload")
				case errors.Is(stateErr, errIdempotencyInFlight):
					return echo.NewHTTPError(http.StatusConflict, "request with this idempotency key is already in progress")
				default:
					return echo.NewHTTPError(http.StatusInternalServerError, "idempotency processing failed")
				}
			}

			if replayRecord != nil {
				c.Response().Header().Set("Idempotency-Status", "replayed")
				c.Response().Header().Set("Idempotency-Replayed", "true")
				contentType := replayRecord.contentType
				if strings.TrimSpace(contentType) == "" {
					contentType = echo.MIMEApplicationJSONCharsetUTF8
				}
				return c.Blob(replayRecord.status, contentType, replayRecord.body)
			}

			recorder := newCaptureWriter(c.Response().Writer)
			c.Response().Writer = recorder

			handlerErr := next(c)
			if handlerErr != nil {
				s.abort(recordKey)
				return handlerErr
			}

			status := recorder.status
			if status == 0 {
				status = http.StatusOK
			}
			if status >= 200 && status < 300 {
				s.complete(recordKey, status, recorder.Header().Get(echo.HeaderContentType), recorder.body.Bytes())
				c.Response().Header().Set("Idempotency-Status", "stored")
			} else {
				s.abort(recordKey)
			}

			return nil
		}
	}
}

var (
	errIdempotencyConflict = errors.New("idempotency conflict")
	errIdempotencyInFlight = errors.New("idempotency in-flight")
)

func (s *Store) beginOrReplay(key, fingerprint string) (*record, error) {
	now := s.now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()

	s.requestCounter++
	if s.requestCounter%s.cleanupEvery == 0 {
		s.pruneExpiredLocked(now)
	}

	existing, ok := s.records[key]
	if ok && now.Before(existing.expiresAt) {
		if existing.fingerprint != fingerprint {
			return nil, errIdempotencyConflict
		}
		if existing.inFlight {
			return nil, errIdempotencyInFlight
		}
		copied := existing
		copied.body = append([]byte(nil), existing.body...)
		return &copied, nil
	}

	s.records[key] = record{
		fingerprint: fingerprint,
		inFlight:    true,
		expiresAt:   now.Add(s.ttl),
	}
	return nil, nil
}

func (s *Store) complete(key string, status int, contentType string, body []byte) {
	now := s.now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.records[key]
	if !ok {
		return
	}
	existing.inFlight = false
	existing.status = status
	existing.contentType = contentType
	existing.body = append([]byte(nil), body...)
	existing.expiresAt = now.Add(s.ttl)
	s.records[key] = existing
}

func (s *Store) abort(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, key)
}

func (s *Store) pruneExpiredLocked(now time.Time) {
	for key, existing := range s.records {
		if !now.Before(existing.expiresAt) {
			delete(s.records, key)
		}
	}
}

func (s *Store) makeRecordKey(c echo.Context, key string) string {
	scope := strings.TrimSpace(s.scope(c))
	if scope == "" {
		scope = "ip:unknown"
	}
	path := c.Path()
	if strings.TrimSpace(path) == "" {
		path = c.Request().URL.Path
	}
	return scope + ":" + c.Request().Method + ":" + path + ":" + key
}

func makeFingerprint(method, path string, body []byte) string {
	digest := sha256.Sum256(body)
	return method + ":" + path + ":" + hex.EncodeToString(digest[:])
}

func readAndRestoreBody(c echo.Context) ([]byte, error) {
	if c.Request().Body == nil {
		return nil, nil
	}
	payload, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return nil, err
	}
	c.Request().Body = io.NopCloser(bytes.NewReader(payload))
	return payload, nil
}

type captureWriter struct {
	writer http.ResponseWriter
	header http.Header
	status int
	body   bytes.Buffer
}

func newCaptureWriter(writer http.ResponseWriter) *captureWriter {
	return &captureWriter{writer: writer}
}

func (w *captureWriter) Header() http.Header {
	if w.header != nil {
		return w.header
	}
	return w.writer.Header()
}

func (w *captureWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.writer.WriteHeader(code)
}

func (w *captureWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	_, _ = w.body.Write(data)
	return w.writer.Write(data)
}

func (w *captureWriter) Flush() {
	if flusher, ok := w.writer.(http.Flusher); ok {
		flusher.Flush()
	}
}
