package utils

import (
	"os"
	"strings"

	logrus "github.com/sirupsen/logrus"
	prefixed "github.com/x-cray/logrus-prefixed-formatter"
)

type Logger struct {
	*logrus.Logger
	Name string
}

var log *logrus.Entry
var loggers map[string]*Logger
var lastLogLevels map[string]string

// DefaultLoggingLevel - set default log level here
var DefaultLoggingLevel = logrus.DebugLevel

// Init Logging
func init() {
	loggers = make(map[string]*Logger)
}

func setLogLevelForLogger(loggerName string, logLevelStr string) {
	var logger *Logger
	var ok bool
	if logger, ok = loggers[loggerName]; !ok {
		return
	}
	var logLevel logrus.Level = DefaultLoggingLevel
	var err error
	if logLevelStr != "" {
		logLevel, err = logrus.ParseLevel(strings.ToLower(logLevelStr))
		if err != nil {
			logrus.Warnf("Cannot parse log level from environment for logger %s", loggerName)
			logLevel = DefaultLoggingLevel
		}
	}
	logrus.Debugf("Setting logger %s level to %d", loggerName, logLevel)
	logger.SetLevel(logLevel)
}

func setLogLevelFromEnv(loggerName string) {
	logLevel := os.Getenv("LOGLEVEL")
	if logLevel == "" {
		logLevel = DefaultLoggingLevel.String()
	}
	setLogLevelForLogger(loggerName, logLevel)
}

// GetLogger - Get instance of logger for logging by specified name
func GetLogger(loggerName string) *Logger {
	var logger *Logger
	if logger, ok := loggers[loggerName]; ok {
		return logger
	}
	logger = &Logger{
		Logger: logrus.New(),
		Name:   loggerName,
	}
	loggers[loggerName] = logger
	formatter := new(prefixed.TextFormatter)
	formatter.DisableColors = true
	formatter.FullTimestamp = true
	formatter.ForceFormatting = true
	formatter.TimestampFormat = "2006-01-02 15:04:05"
	logger.SetFormatter(formatter)
	logger.SetReportCaller(true)
	setLogLevelFromEnv(loggerName)
	log = GetLogEntry(loggerName)
	return logger
}

func GetLogEntry(name string) *logrus.Entry {
	return GetLogger(name).WithFields(logrus.Fields{"Service": name})
}
