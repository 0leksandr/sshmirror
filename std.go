package main

import (
	"fmt"
	"runtime"
	"time"
)

func PanicIf(err error) {
	if err != nil {
		Dump(err)
		panic(err)
	}
}

func Dump(val interface{}) {
	_, file, line, _ := runtime.Caller(1)
	if err, ok := val.(error); ok {
		val = err.Error()
	}
	fmt.Printf("%v:%d %#v\n", file, line, val)
}

type ProgressBar struct {
	description string
	total       int
	current     int
	width       int
	divider     int // TODO: rename
	start       time.Time
}

func NewProgressBar(description string, totalSteps int) ProgressBar {
	max := func(a int, b int) int {
		if a > b {
			return a
		} else {
			return b
		}
	}
	width := 100

	bar := ProgressBar{
		description: description,
		total:       totalSteps,
		current:     0,
		width:       width,
		divider:     max(1, totalSteps/width),
		start:       time.Now(),
	}
	bar.print("")

	return bar
}
func (bar *ProgressBar) Add() {
	bar.current++
	if bar.current%bar.divider == 0 {
		bar.print("")
	}
	if bar.current == bar.total {
		bar.print(" " + time.Since(bar.start).String() + "\n")
	}
}
func (bar *ProgressBar) print(postfix string) {
	curNotches := bar.current * bar.width / bar.total
	getBar := func(symbol string, nrSymbols int) string {
		_bar := ""
		for c := 0; c < nrSymbols; c++ {
			_bar += symbol
		}
		return _bar
	}
	_bar := getBar("|", curNotches) + getBar(".", bar.width-curNotches)
	fmt.Print("\r" + bar.description + ": [" + _bar + "]" + postfix)
}
func testProgress() {
	for _, nrSteps := range []int{1, 3, 10, 11, 30, 100, 300, 999, 1000, 1001} {
		sleep := int(1*time.Second) / nrSteps
		progress := NewProgressBar(fmt.Sprintf("test %4d", nrSteps), nrSteps)
		for c := 0; c < nrSteps; c++ {
			time.Sleep(time.Duration(sleep))
			progress.Add()
		}
	}
}
