package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"github.com/fsnotify/fsnotify"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

type Listener interface { // TODO: rename to `Watcher`
	io.Closer
	Modifications() <-chan Modification
	Fallback() []string // MAYBE: rename; test
}

type ListenerFactory struct {
	LoggerAware
	logger Logger
}
func (ListenerFactory) New() ListenerFactory {
	return ListenerFactory{
		logger: NullLogger{},
	}
}
func (factory ListenerFactory) SetLogger(logger Logger) {
	factory.logger = logger
}
func (factory ListenerFactory) Get(root string, exclude string) Listener {
	if inotify, err := (InotifyListener{}.New(root, exclude)); err == nil {
		return inotify
	} else {
		Must(inotify.Close())
		factory.logger.Error(err.Error())
		factory.logger.Error(
			"Warning! Current FS events provider: fsnotify. It has known problem of not tracking contents of " +
				"subdirectories, created after program was started. It can only reliably track files in existing " +
				"subdirectories",
		)
		return FsnotifyListener{}.New(root, exclude)
	}
}

type FsnotifyListener struct { // TODO: watch new subdirectories!
	Listener
	io.Closer
	modifications     chan Modification
	modifiedFilenames []string // MAYBE: separate type for filenames
	stopWatching      func()
}
func (FsnotifyListener) New(root string, exclude string) Listener {
	var ignored *regexp.Regexp
	if exclude != "" { ignored = regexp.MustCompile(exclude) }
	listener := FsnotifyListener{
		modifications: make(chan Modification),
	}
	var events <-chan fsnotify.Event
	events, listener.stopWatching = FsnotifyListener{}.watchDirRecursive(root, ignored)

	var processEvent func(event fsnotify.Event)
	processEvent = func(event fsnotify.Event) {
		if event.Op == 0 { return } // MAYBE: report? This is weird
		if event.Op == fsnotify.Chmod { return }
		filename := event.Name[len(root)+1:]
		if ignored != nil && ignored.MatchString(filename) { return }

		switch event.Op {
			case fsnotify.Create, fsnotify.Write:
				listener.put(Updated{filename: filename})
			case fsnotify.Remove:
				listener.put(Deleted{filename: filename})
			case fsnotify.Rename:
				putDefault := func() { listener.put(Deleted{filename: filename}) }
				select {
					case nextEvent, ok := <-events:
						if ok {
							if nextEvent.Op == fsnotify.Create { // MAYBE: check contents (checksums, modification times)
								listener.put(Moved{
									from: filename,
									to:   nextEvent.Name[len(root)+1:],
								})
							} else {
								putDefault()
								processEvent(nextEvent)
							}
						} else {
							putDefault()
						}
					case <-time.After(1 * time.Millisecond): // MAYBE: tweak
						putDefault()
					// MAYBE: listen for exit
				}
			default:
				panic("Unknown event")
		}

		listener.modifiedFilenames = append(listener.modifiedFilenames, filename)
	}

	go func() {
		for {
			select {
				case event, ok := <-events:
					if !ok { return }
					processEvent(event)
			}
		}
	}()

	return &listener
}
func (listener *FsnotifyListener) Close() error {
	listener.stopWatching()
	close(listener.modifications)
	return nil
}
func (listener *FsnotifyListener) Modifications() <-chan Modification {
	return listener.modifications
}
func (listener *FsnotifyListener) Fallback() []string {
	modifiedFilenames := listener.modifiedFilenames
	listener.modifiedFilenames = make([]string, 0)
	return modifiedFilenames
}
func (FsnotifyListener) watchDirRecursive(
	path string,
	ignored *regexp.Regexp,
) (<-chan fsnotify.Event, context.CancelFunc) {
	watcher, err := fsnotify.NewWatcher()
	PanicIf(err)

	isIgnored := func(path2 string) bool {
		if ignored == nil { return false }
		if path2[:len(path)] != path {
			panic(fmt.Sprintf("Unexpected local path: %s", path2))
		}
		path2 = path2[len(path):]
		if path2 != "" {
			// TODO: platform-specific directory separators
			if path2[0] != '/' { panic(fmt.Sprintf("Unexpected local path: %s", path2)) }
			path2 = path2[1:]
		}
		return ignored.MatchString(path2)
	}

	Must(filepath.Walk(
		path,
		func(path string, fi os.FileInfo, err error) error {
			if isIgnored(path) { return nil }
			PanicIf(err)
			if fi.Mode().IsDir() { return watcher.Add(path) }
			return nil
		},
	))

	go func() {
		for {
			select {
				case err2, open := <-watcher.Errors:
					if !open { return }
					PanicIf(err2) // MAYBE: return errors channel
			}
		}
	}()

	return watcher.Events, func() {
		Must(watcher.Close())
	}
}
func (listener *FsnotifyListener) put(modification Modification) { // MAYBE: remove
	listener.modifications <- modification
}

type InotifyListener struct {
	Listener
	io.Closer
	LoggerAware
	modifications     chan Modification
	inotifyProcess    *os.Process
	modifiedFilenames []string
	logger            Logger
}
func (InotifyListener) New(root string, exclude string) (Listener, error) {
	listener := InotifyListener{
		modifications: make(chan Modification), // MAYBE: reserve size
		logger:        NullLogger{},
	}

	const CloseWrite = "CLOSE_WRITE"
	const Delete = "DELETE"
	const MovedFrom = "MOVED_FROM"
	const MovedTo = "MOVED_TO"

	args := []string{
		"--monitor",
		"--recursive",
		"--format", "%w%f\t%e",
		"--event", CloseWrite,
		"--event", Delete,
		"--event", MovedFrom,
		"--event", MovedTo,
	}
	if exclude != "" {
		args = append(args, "--exclude", exclude) // TODO: test
	}
	command := exec.Command("inotifywait", append(args, "--", root)...)

	type Event struct {
		eventType string // MAYBE: tinyint
		filename  string
	}
	events := make(chan Event) // MAYBE: reserve size

	stdout, err1 := command.StdoutPipe()
	PanicIf(err1)
	stdoutScanner := bufio.NewScanner(stdout)
	go func() { // stdout to events
		for stdoutScanner.Scan() {
			line := stdoutScanner.Text()
			listener.logger.Debug("inotify.line", line)
			reg := regexp.MustCompile(fmt.Sprintf("^%s/(.+)\t([^\t]+)$", root)) // TODO: strip trail slash
			parts := reg.FindStringSubmatch(line)
			filename := parts[1]
			eventsStr := parts[2]
			eventType, err := func() (string, error) {
				for _, eventType := range strings.Split(eventsStr, ",") {
					for _, knownType := range []string{
						CloseWrite,
						Delete,
						MovedFrom,
						MovedTo,
					}{
						if eventType == knownType { return eventType, nil }
					}
				}
				return "", errors.New("unknown event: " + eventsStr)
			}()
			if err == nil {
				events <- Event{
					eventType: eventType,
					filename:  filename,
				}
			} else {
				listener.logger.Error(err.Error())
			}
		}

		close(events)
	}()

	put := func(modification Modification) { listener.modifications <- modification }
	var processEvent func(Event)
	processEvent = func(event Event) {
		listener.logger.Debug("event", event)
		filename := event.filename
		switch event.eventType {
			case CloseWrite: put(Updated{filename})
			case Delete: put(Deleted{filename})
			case MovedFrom:
				putDefault := func() { put(Deleted{filename}) }
				select {
					case nextEvent, ok := <- events:
						if ok {
							if nextEvent.eventType == MovedTo {
								put(Moved{
									from: filename,
									to:   nextEvent.filename,
								})
							} else {
								putDefault()
								processEvent(nextEvent)
							}
						} else {
							putDefault()
						}
					case <-time.After(2 * time.Millisecond): // MAYBE: tweak
						putDefault()
					// MAYBE: listen for exit
				}
			case MovedTo: put(Updated{filename})
		}

		listener.modifiedFilenames = append(listener.modifiedFilenames, filename)
	}
	go func() { // events to modifications
		for {
			select {
				case event, ok := <-events:
					if ok {
						processEvent(event)
					} else {
						close(listener.modifications)
						return
					}
			}
		}
	}()

	err := command.Start()
	listener.inotifyProcess = command.Process

	// TODO: read error/info stream, await for watches to establish
	// TODO: calculate and check nr of files to be watched, see https://www.baeldung.com/linux/inotify-upper-limit-reached
	return &listener, err
}
func (listener *InotifyListener) Close() error {
	return listener.inotifyProcess.Signal(syscall.SIGTERM)
}
func (listener *InotifyListener) Modifications() <-chan Modification {
	return listener.modifications
}
func (listener *InotifyListener) Fallback() []string {
	modifiedFilenames := listener.modifiedFilenames
	listener.modifiedFilenames = []string{}
	return modifiedFilenames
}
