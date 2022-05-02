package main

import (
	"fmt"
	"github.com/0leksandr/my.go"
	"sync"
	"time"
)

type ErrorLogger interface {
	Error(string)
}

type StdErrLogger struct {
	ErrorLogger
}
func (logger StdErrLogger) Error(err string) {
	my.WriteToStderr(err)
}

type ErrorCmdLogger struct {
	ErrorLogger
	errorCmd string
}
func (logger ErrorCmdLogger) Error(err string) {
	my.RunCommand("", logger.errorCmd + " " + err, nil, nil)
}

type Logger interface {
	ErrorLogger
	Debug(string, ...interface{})
}

type InMemoryLogger struct {
	Logger
	logs        []string
	timestamps  bool
	mutex       sync.Mutex
	errorLogger ErrorLogger
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
	logger.errorLogger.Error(err)
}
func (logger *InMemoryLogger) Print() {
	logger.mutex.Lock()
	for _, log := range logger.logs {
		fmt.Println(log)
	}
	logger.mutex.Unlock()
}
func (logger *InMemoryLogger) log(text string) {
	log := my.Trace(false)[2].String()
	if logger.timestamps { log += ": " + time.Now().String() }
	log += "\n" + text
	logger.logs = append(logger.logs, log)
}

type NullLogger struct {
	Logger
	errorLogger ErrorLogger
}
func (logger NullLogger) Debug(string, ...interface{}) {}
func (logger NullLogger) Error(err string) {
	logger.errorLogger.Error(err)
}

type LoggerAware interface {
	SetLogger(Logger)
}
