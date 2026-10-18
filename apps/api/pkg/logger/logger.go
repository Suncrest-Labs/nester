package logger

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/suncrestlabs/nester/apps/api/internal/config"
)

const serviceName = "nester-api"

func New(cfg config.LogConfig, version string) (*slog.Logger, error) {
	level, err := parseLevel(cfg.Level())
	if err != nil {
		return nil, err
	}

	handlerOptions := &slog.HandlerOptions{
		Level: level,
	}

	handler, err := newHandler(cfg.Format(), os.Stdout, handlerOptions)
	if err != nil {
		return nil, err
	}

	baseLogger := slog.New(handler).With("service", serviceName, "version", version)
	slog.SetDefault(baseLogger)
	return baseLogger, nil
}

func newHandler(format string, destination io.Writer, options *slog.HandlerOptions) (slog.Handler, error) {
	opts := &slog.HandlerOptions{
		ReplaceAttr: RedactReplaceAttr,
	}
	if options != nil {
		cp := *options
		if cp.ReplaceAttr == nil {
			cp.ReplaceAttr = RedactReplaceAttr
		}
		opts = &cp
	}

	switch strings.ToLower(format) {
	case "json":
		return slog.NewJSONHandler(destination, opts), nil
	case "text":
		return slog.NewTextHandler(destination, opts), nil
	default:
		return nil, fmt.Errorf("unsupported log format %q", format)
	}
}

func parseLevel(level string) (slog.Level, error) {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("unsupported log level %q", level)
	}
}
