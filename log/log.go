package log

import (
	"go.uber.org/zap"
)

var logger *zap.SugaredLogger

func init() {
	logger = zap.NewExample().Sugar()
}

func GetLogger() *zap.SugaredLogger {
	return logger
}

func SyncLogger() {
	logger.Sync()
}
