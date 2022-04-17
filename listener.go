package main

import (
	"context"
	"fmt"
	"github.com/fsnotify/fsnotify"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

type Listener interface {
	io.Closer
	Modifications() <-chan Modification
	Fallback() []string // MAYBE: rename; test
}

type FsnotifyListener struct {
	Listener
	modifications     chan Modification
	modifiedFilenames []string // MAYBE: separate type for filenames
	stopWatching      func()
}
func (FsnotifyListener) New(root string, ignored *regexp.Regexp) Listener {
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
