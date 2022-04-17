package main

import (
	"fmt"
	"github.com/0leksandr/my.go"
	"sync"
	"time"
)

type Logger interface {
	Debug(string, ...interface{})
	Error(string)
}

type LoggerAware interface {
	SetLogger(Logger)
}

type InMemoryLogger struct {
	Logger
	logs       []string
	timestamps bool
	mutex      sync.Mutex
}
func (logger *InMemoryLogger) Debug(message string, values ...interface{}) {
	switch len(values) {
		case 0:
			logger.log(message)
		case 1:
			logger.log(message + ": " + my.Sdump2(values[0]))
		default:
			logger.mutex.Lock()
			logger.log(message)
			for _, value := range values {
				logger.log(my.Sdump2(value))
			}
			logger.mutex.Unlock()
	}
}
func (logger *InMemoryLogger) Error(err string) {
	logger.log("Error: " + err)
	my.WriteToStderr(err)
}
func (logger *InMemoryLogger) Print() {
	logger.mutex.Lock()
	for _, log := range logger.logs {
		fmt.Println(log)
	}
	logger.mutex.Unlock()
}
func (logger *InMemoryLogger) log(text string) {
	var log string
	trace := my.Trace(false)
	switch true {
		case len(trace) > 2: log = trace[2]
		case len(trace) > 0: log = trace[len(trace)-1]
		default:             log = "[INVALID TRACE]"
	}
	if logger.timestamps { log += ": " + time.Now().String() }
	log += "\n" + text
	logger.logs = append(logger.logs, log)
}

type NullLogger struct {
	Logger
}
func (logger NullLogger) Debug(string, ...interface{}) {
}
func (logger NullLogger) Error(err string) {
	my.WriteToStderr(err)
}
