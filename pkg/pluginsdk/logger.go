package pluginsdk

import (
	"fmt"
	"log"
)

type defaultLogger struct {
	prefix string
}

// NewDefaultLogger creates a scoped logger prefixed with [plugin-id].
func NewDefaultLogger(pluginID string) Logger {
	if pluginID == "" {
		pluginID = "plugin"
	}
	return &defaultLogger{
		prefix: fmt.Sprintf("[%s] ", pluginID),
	}
}

func (l *defaultLogger) Debugf(format string, args ...any) {
	log.Printf(l.prefix+"DEBUG: "+format, args...)
}

func (l *defaultLogger) Infof(format string, args ...any) {
	log.Printf(l.prefix+"INFO: "+format, args...)
}

func (l *defaultLogger) Warnf(format string, args ...any) {
	log.Printf(l.prefix+"WARN: "+format, args...)
}

func (l *defaultLogger) Errorf(format string, args ...any) {
	log.Printf(l.prefix+"ERROR: "+format, args...)
}
