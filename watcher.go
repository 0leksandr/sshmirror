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

type Watcher interface {
	io.Closer
	Modifications() <-chan Modification
}

type FsnotifyWatcher struct { // PRIORITY: watch new subdirectories
	Watcher
	io.Closer
	modifications chan Modification
	stopWatching  func()
}
func (FsnotifyWatcher) New(root string, exclude string) Watcher {
	var ignored *regexp.Regexp
	if exclude != "" { ignored = regexp.MustCompile(exclude) }
	watcher := FsnotifyWatcher{
		modifications: make(chan Modification),
	}
	var events <-chan fsnotify.Event
	events, watcher.stopWatching = FsnotifyWatcher{}.watchDirRecursive(root, ignored)

	var processEvent func(event fsnotify.Event)
	processEvent = func(event fsnotify.Event) {
		if event.Op == 0 { return } // MAYBE: report? This is weird
		if event.Op == fsnotify.Chmod { return }
		filename := event.Name[len(root)+1:]
		if ignored != nil && ignored.MatchString(filename) { return }

		switch event.Op {
			case fsnotify.Create, fsnotify.Write:
				watcher.put(Updated{filename: filename})
			case fsnotify.Remove:
				watcher.put(Deleted{filename: filename})
			case fsnotify.Rename:
				putDefault := func() { watcher.put(Deleted{filename: filename}) }
				select {
					case nextEvent, ok := <-events:
						if ok {
							if nextEvent.Op == fsnotify.Create { // MAYBE: check contents (checksums, modification times)
								watcher.put(Moved{
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

	return &watcher
}
func (watcher *FsnotifyWatcher) Close() error {
	watcher.stopWatching()
	close(watcher.modifications)
	return nil
}
func (watcher *FsnotifyWatcher) Modifications() <-chan Modification {
	return watcher.modifications
}
func (FsnotifyWatcher) watchDirRecursive(
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
func (watcher *FsnotifyWatcher) put(modification Modification) { // MAYBE: remove
	watcher.modifications <- modification
}

type InotifyWatcher struct {
	Watcher
	io.Closer
	modifications  chan Modification
	inotifyProcess *os.Process
	logger         Logger
}
func (InotifyWatcher) New(root string, exclude string, logger Logger) (Watcher, error) {
	watcher := InotifyWatcher{
		modifications: make(chan Modification), // MAYBE: reserve size
		logger:        logger,
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
			watcher.logger.Debug("inotify.line", line)
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
				watcher.logger.Error(err.Error())
			}
		}

		close(events)
	}()

	put := func(modification Modification) { watcher.modifications <- modification }
	var processEvent func(Event)
	processEvent = func(event Event) {
		watcher.logger.Debug("event", event)
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
	}
	go func() { // events to modifications
		for {
			select {
				case event, ok := <-events:
					if ok {
						processEvent(event)
					} else {
						close(watcher.modifications)
						return
					}
			}
		}
	}()

	err := command.Start()
	watcher.inotifyProcess = command.Process

	// PRIORITY: calculate and check nr of files to be watched, see https://www.baeldung.com/linux/inotify-upper-limit-reached
	// TODO: read error/info stream, await for watches to establish
	return &watcher, err
}
func (watcher *InotifyWatcher) Close() error {
	return watcher.inotifyProcess.Signal(syscall.SIGTERM)
}
func (watcher *InotifyWatcher) Modifications() <-chan Modification {
	return watcher.modifications
}
