package logger

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type level int

const (
	levelInfo level = iota
	levelTrace
	levelError
)

type ctxKey struct{}

// Default is the process-wide logger instance.
var Default *Logger

// Logger writes daily rotated log files:
//   - YYYY-MM-DD.log        general logs
//   - YYYY-MM-DD.log.trace  important workflow / pipeline logs
//   - YYYY-MM-DD.log.error  errors
type Logger struct {
	dir string
	mu  sync.Mutex

	day       string
	infoFile  *os.File
	traceFile *os.File
	errFile   *os.File
}

// Init creates the default logger under dir (e.g. ./logs).
func Init(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	Default = &Logger{dir: dir}
	return Default.rotateIfNeeded()
}

// NewID generates a unique log id for one request/operation chain.
func NewID() string {
	return fmt.Sprintf("log_%d", time.Now().UnixNano())
}

// WithID attaches log id to context.
func WithID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// InheritID copies log id from parent onto child when present.
func InheritID(parent, child context.Context) context.Context {
	if parent == nil || child == nil {
		return child
	}
	if id := IDFromContext(parent); id != "" {
		return WithID(child, id)
	}
	return child
}

// IDFromContext returns log id from context, or empty string.
func IDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(ctxKey{}).(string)
	return v
}

// Info writes a line to YYYY-MM-DD.log.
func (l *Logger) Info(logID, msg string) {
	l.write(levelInfo, logID, msg)
}

// Trace writes a line to YYYY-MM-DD.log.trace.
func (l *Logger) Trace(logID, msg string) {
	l.write(levelTrace, logID, msg)
}

// Error writes a line to YYYY-MM-DD.log.error.
func (l *Logger) Error(logID, msg string, err error) {
	line := msg
	if err != nil {
		line = fmt.Sprintf("%s err=%v", msg, err)
	}
	l.write(levelError, logID, line)
}

// CtxInfo logs general info with id from context → .log
func CtxInfo(ctx context.Context, format string, args ...interface{}) {
	if Default == nil {
		return
	}
	Default.Info(IDFromContext(ctx), fmt.Sprintf(format, args...))
}

// CtxTrace logs workflow / pipeline steps with id from context → .log.trace
func CtxTrace(ctx context.Context, format string, args ...interface{}) {
	if Default == nil {
		return
	}
	Default.Trace(IDFromContext(ctx), fmt.Sprintf(format, args...))
}

// CtxError logs error with id from context → .log.error
func CtxError(ctx context.Context, err error, format string, args ...interface{}) {
	if Default == nil {
		return
	}
	Default.Error(IDFromContext(ctx), fmt.Sprintf(format, args...), err)
}

func (l *Logger) write(lv level, logID, msg string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := l.rotateIfNeeded(); err != nil {
		return
	}

	ts := time.Now().Format("2006-01-02 15:04:05.000")
	line := fmt.Sprintf("%s [log_id=%s] %s\n", ts, logID, msg)

	var f *os.File
	switch lv {
	case levelError:
		f = l.errFile
	case levelTrace:
		f = l.traceFile
	default:
		f = l.infoFile
	}
	if f != nil {
		_, _ = f.WriteString(line)
	}
}

func (l *Logger) rotateIfNeeded() error {
	day := time.Now().Format("2006-01-02")
	if l.day == day && l.infoFile != nil && l.traceFile != nil && l.errFile != nil {
		return nil
	}

	if l.infoFile != nil {
		_ = l.infoFile.Close()
	}
	if l.traceFile != nil {
		_ = l.traceFile.Close()
	}
	if l.errFile != nil {
		_ = l.errFile.Close()
	}

	infoPath := filepath.Join(l.dir, day+".log")
	tracePath := filepath.Join(l.dir, day+".log.trace")
	errPath := filepath.Join(l.dir, day+".log.error")

	infoFile, err := os.OpenFile(infoPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	traceFile, err := os.OpenFile(tracePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		_ = infoFile.Close()
		return err
	}
	errFile, err := os.OpenFile(errPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		_ = infoFile.Close()
		_ = traceFile.Close()
		return err
	}

	l.day = day
	l.infoFile = infoFile
	l.traceFile = traceFile
	l.errFile = errFile
	return nil
}
