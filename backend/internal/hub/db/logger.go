package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type zerologGormLogger struct {
	logger                    zerolog.Logger
	slowThreshold             time.Duration
	ignoreRecordNotFoundError bool
	logLevel                  gormlogger.LogLevel
}

type GormLoggerConfig struct {
	SlowThreshold             time.Duration
	IgnoreRecordNotFoundError bool
	LogLevel                  gormlogger.LogLevel
}

func NewGormLogger(logger zerolog.Logger, cfg GormLoggerConfig) gormlogger.Interface {
	return &zerologGormLogger{
		logger:                    logger,
		slowThreshold:             cfg.SlowThreshold,
		ignoreRecordNotFoundError: cfg.IgnoreRecordNotFoundError,
		logLevel:                  cfg.LogLevel,
	}
}

func (l *zerologGormLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	newLogger := *l
	newLogger.logLevel = level
	return &newLogger
}

func (l *zerologGormLogger) Info(_ context.Context, msg string, args ...interface{}) {
	if l.logLevel >= gormlogger.Info {
		l.logger.Info().Msg(fmt.Sprintf(msg, args...))
	}
}

func (l *zerologGormLogger) Warn(_ context.Context, msg string, args ...interface{}) {
	if l.logLevel >= gormlogger.Warn {
		l.logger.Warn().Msg(fmt.Sprintf(msg, args...))
	}
}

func (l *zerologGormLogger) Error(_ context.Context, msg string, args ...interface{}) {
	if l.logLevel >= gormlogger.Error {
		l.logger.Error().Msg(fmt.Sprintf(msg, args...))
	}
}

func (l *zerologGormLogger) Trace(_ context.Context, begin time.Time, fc func() (string, int64), err error) {
	if l.logLevel <= gormlogger.Silent {
		return
	}

	elapsed := time.Since(begin)
	sql, rows := fc()

	switch {
	case err != nil && l.logLevel >= gormlogger.Error &&
		(!l.ignoreRecordNotFoundError || !errors.Is(err, gorm.ErrRecordNotFound)):
		l.logger.Error().
			Err(err).
			Dur("elapsed", elapsed).
			Int64("rows", rows).
			Str("sql", sql).
			Msg("query error")

	case l.slowThreshold > 0 && elapsed > l.slowThreshold && l.logLevel >= gormlogger.Warn:
		l.logger.Warn().
			Dur("elapsed", elapsed).
			Dur("threshold", l.slowThreshold).
			Int64("rows", rows).
			Str("sql", sql).
			Msg("slow query")

	case l.logLevel >= gormlogger.Info:
		l.logger.Debug().
			Dur("elapsed", elapsed).
			Int64("rows", rows).
			Str("sql", sql).
			Msg("query")
	}
}
