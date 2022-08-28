package main

import (
	"fmt"
	"github.com/0leksandr/my.go"
	"regexp"
	"sync"
	"time"
)

type Logger struct {
	debug DebugLogger
	error ErrorLogger
}
func (logger Logger) Debug(message string, values ...interface{}) {
	logger.debug.Debug(message, values...)
}
func (logger Logger) Error(err string) {
	logger.error.Error(err)
}

type ErrorLogger interface {
	Error(string)
}

type StdErrLogger struct {
	formatter LogFormatter
}
func (logger StdErrLogger) Error(err string) {
	my.WriteToStderr(logger.formatter.Format(err)[0])
}

type ErrorCmdLogger struct {
	errorCmd string
}
func (logger ErrorCmdLogger) Error(err string) {
	my.RunCommand("", logger.errorCmd + " " + err, nil, nil)
}

type InMemoryErrorLogger struct {
	collector InMemoryLogCollector
}
func (logger *InMemoryErrorLogger) Error(err string) {
	logger.collector.Add("Error: " + err)
}

type ComboErrorLogger struct {
	loggers []ErrorLogger
}
func (logger ComboErrorLogger) Error(err string) {
	for _, _logger := range logger.loggers {
		_logger.Error(err)
	}
}

type DebugLogger interface {
	Debug(string, ...interface{})
}

type InMemoryDebugLogger struct {
	formatter LogFormatter
	collector InMemoryLogCollector
}
func (logger *InMemoryDebugLogger) Debug(message string, values ...interface{}) {
	for _, log := range logger.formatter.Format(message, values...) {
		logger.collector.Add(log)
	}
}

type StdOutLogger struct {
	formatter LogFormatter
}
func (logger StdOutLogger) Debug(message string, values ...interface{}) {
	for _, log := range logger.formatter.Format(message, values...) {
		fmt.Println(log)
	}
}

type NullLogger struct {}
func (logger NullLogger) Debug(string, ...interface{}) {}

type ComboDebugLogger struct {
	loggers []DebugLogger
}
func (logger ComboDebugLogger) Debug(message string, values ...interface{}) {
	for _, _logger := range logger.loggers {
		_logger.Debug(message, values...)
	}
}

type InMemoryLogCollector struct {
	logs      []string
	mutex     sync.Mutex
	formatter LogFormatter
}
func (logger *InMemoryLogCollector) Add(text string) {
	logger.logs = append(logger.logs, text)
}
func (logger *InMemoryLogCollector) Print() {
	logger.mutex.Lock()
	for _, log := range logger.logs {
		fmt.Println(log)
	}
	logger.mutex.Unlock()
}
func (logger *InMemoryLogCollector) Clear() {
	logger.logs = []string{}
}

type LogFormatter struct {
	timestamps bool
}
func (formatter LogFormatter) Format(message string, values ...interface{}) []string {
	switch len(values) {
		case 0:
			return []string{formatter.addTrace(message)}
		case 1:
			return []string{formatter.addTrace(message + ": " + my.Sdump2(values[0]))}
		default:
			formatted := make([]string, 0, 1 + len(values))
			formatted = append(formatted, formatter.addTrace(message))
			for _, value := range values {
				formatted = append(formatted, formatter.addTrace(my.Sdump2(value)))
			}
			return formatted
	}
}
func (formatter LogFormatter) addTrace(text string) string {
	trace := my.Trace{}.New().SkipFile(1)[0].String()
	if formatter.timestamps {
		t := time.Now().String()
		t = regexp.MustCompile(" m=\\+([^ ]+)$").FindStringSubmatch(t)[1]
		trace += ": " + t
	}
	return trace + "\n" + text
}
