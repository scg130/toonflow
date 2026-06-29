package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"toonflow/logger"

	"github.com/gin-gonic/gin"
)

const ginLogIDKey = "logID"

// LogID returns the request-scoped log id from gin context.
func LogID(c *gin.Context) string {
	v, _ := c.Get(ginLogIDKey)
	s, _ := v.(string)
	return s
}

type bodyWriter struct {
	gin.ResponseWriter
	body   bytes.Buffer
	status int
}

func (w *bodyWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(b)
}

func (w *bodyWriter) WriteHeader(code int) {
	w.status = code
}

func (w *bodyWriter) flush(logID string) {
	status := w.status
	if status == 0 {
		status = http.StatusOK
	}

	raw := w.body.Bytes()
	if len(raw) == 0 {
		w.ResponseWriter.WriteHeader(status)
		return
	}

	ct := w.ResponseWriter.Header().Get("Content-Type")
	if strings.Contains(ct, "application/json") {
		out := injectLogID(raw, logID)
		w.ResponseWriter.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.ResponseWriter.WriteHeader(status)
		_, _ = w.ResponseWriter.Write(out)
		return
	}

	w.ResponseWriter.WriteHeader(status)
	_, _ = w.ResponseWriter.Write(raw)
}

func injectLogID(raw []byte, logID string) []byte {
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw
	}
	out := addLogID(v, logID)
	b, err := json.Marshal(out)
	if err != nil {
		return raw
	}
	return b
}

func addLogID(v interface{}, logID string) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		if _, ok := t["log_id"]; !ok {
			t["log_id"] = logID
		}
		return t
	default:
		return gin.H{"log_id": logID, "data": v}
	}
}

// RequestLogMiddleware assigns a log id per request and writes access logs.
func RequestLogMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		logID := logger.NewID()
		c.Set(ginLogIDKey, logID)
		c.Header("X-Log-ID", logID)
		c.Request = c.Request.WithContext(logger.WithID(c.Request.Context(), logID))

		start := time.Now()
		if logger.Default != nil {
			logger.Default.Info(logID, "request start method="+c.Request.Method+" path="+c.Request.URL.Path+" ip="+c.ClientIP())
		}

		blw := &bodyWriter{ResponseWriter: c.Writer}
		c.Writer = blw

		c.Next()

		status := blw.status
		if status == 0 {
			status = http.StatusOK
		}
		blw.flush(logID)

		latency := time.Since(start)
		msg := "request end status=" + itoa(status) + " latency=" + latency.String()
		if logger.Default != nil {
			if status >= 500 {
				logger.Default.Error(logID, msg, nil)
			} else {
				logger.Default.Info(logID, msg)
			}
			if len(c.Errors) > 0 {
				for _, e := range c.Errors {
					logger.Default.Error(logID, "handler error", e.Err)
				}
			}
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
