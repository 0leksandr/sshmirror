package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"github.com/0leksandr/my.go"
	"github.com/fsnotify/fsnotify"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Watcher interface {
	io.Closer
	Modifications() <-chan Modification
	Name() string
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
		filenameString := event.Name[len(root)+1:]
		if ignored != nil && ignored.MatchString(filenameString) { return }
		filename := Filename(filenameString)

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
									to:   Filename(nextEvent.Name[len(root)+1:]),
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
		for event := range events {
			processEvent(event)
		}
	}()

	return &watcher
}
func (FsnotifyWatcher) Name() string {
	return "fsnotify"
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
		PanicIf(<-watcher.Errors) // MAYBE: return errors channel
	}()

	return watcher.Events, func() { Must(watcher.Close()) }
}
func (watcher *FsnotifyWatcher) put(modification Modification) { // MAYBE: remove
	watcher.modifications <- modification
}

type InotifyWatcher struct {
	Watcher
	io.Closer
	modifications chan Modification
	logger        Logger
	onClose       func() error
}
func (InotifyWatcher) New(root string, exclude string, logger Logger) (Watcher, error) {
	modifications := make(chan Modification) // MAYBE: reserve size
	watcher := InotifyWatcher{
		modifications: modifications,
		logger:        logger,
		onClose: func() error {
			close(modifications)
			return nil
		},
	}

	nrFiles, errCalculateFiles := watcher.getNrFiles(root)
	if errCalculateFiles != nil { return &watcher, errCalculateFiles }
	maxUserWatchers, errMaxUserWatchers := watcher.getMaxUserWatchers()
	if errMaxUserWatchers != nil { return &watcher, errMaxUserWatchers }
	requiredNrWatchers := watcher.getRequiredNrWatchers(nrFiles)
	if requiredNrWatchers > maxUserWatchers { // THINK: https://www.baeldung.com/linux/inotify-upper-limit-reached
		if err := watcher.setMaxUserWatchers(requiredNrWatchers); err != nil { return &watcher, err }
	}

	const CloseWriteStr = "CLOSE_WRITE"
	const DeleteStr     = "DELETE"
	const MovedFromStr  = "MOVED_FROM"
	const MovedToStr    = "MOVED_TO"

	type EventType uint8
	const (
		CloseWriteCode EventType = 1 << iota
		DeleteCode
		MovedFromCode
		MovedToCode
	)

	args := []string{
		"--monitor",
		"--recursive",
		"--format", "%w%f\t%e",
		"--event", CloseWriteStr,
		"--event", DeleteStr,
		"--event", MovedFromStr,
		"--event", MovedToStr,
	}
	if exclude != "" {
		args = append(args, "--exclude", exclude) // TODO: test
	}
	command := exec.Command("inotifywait", append(args, "--", root)...)

	type Event struct {
		eventType EventType
		filename  string
	}
	events := make(chan Event) // MAYBE: reserve size
	watcher.onClose = func() error {
		close(events)
		close(modifications)
		return nil
	}

	stdout, err1 := command.StdoutPipe()
	PanicIf(err1)
	stdoutScanner := bufio.NewScanner(stdout)
	reg := regexp.MustCompile(fmt.Sprintf("^%s(.+)\t([^\t]+)$", stripTrailSlash(root) + string(os.PathSeparator)))
	knownTypes := []struct {
		str  string
		code EventType
	}{
		{CloseWriteStr, CloseWriteCode},
		{DeleteStr,     DeleteCode    },
		{MovedFromStr,  MovedFromCode },
		{MovedToStr,    MovedToCode   },
	}
	go func() { // stdout to events
		for stdoutScanner.Scan() {
			line := stdoutScanner.Text()
			watcher.logger.Debug("inotify.line", line)
			parts := reg.FindStringSubmatch(line)
			filename := parts[1]
			eventsStr := parts[2]
			eventType, errReadEvent := func() (EventType, error) {
				for _, eventType := range strings.Split(eventsStr, ",") {
					for _, knownType := range knownTypes {
						if eventType == knownType.str { return knownType.code, nil }
					}
				}
				return 0, errors.New("unknown event: " + eventsStr)
			}()
			if errReadEvent == nil {
				events <- Event{
					eventType: eventType,
					filename:  filename,
				}
			} else {
				watcher.logger.Error(errReadEvent.Error())
			}
		}

		close(events)
	}()

	put := func(modification Modification) { watcher.modifications <- modification }
	var processEvent func(Event)
	processEvent = func(event Event) {
		watcher.logger.Debug("event", event)
		filename := Filename(event.filename)
		switch event.eventType {
			case CloseWriteCode: put(Updated{filename})
			case DeleteCode: put(Deleted{filename})
			case MovedFromCode:
				putDefault := func() { put(Deleted{filename}) }
				select {
					case nextEvent, ok := <- events:
						if ok {
							if nextEvent.eventType == MovedToCode {
								put(Moved{
									from: filename,
									to:   Filename(nextEvent.filename),
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
			case MovedToCode: put(Updated{filename})
		}
	}
	go func() { // events to modifications
		for event := range events { processEvent(event) }
		close(watcher.modifications)
		return
	}()

	errCommandStart := command.Start()
	watcher.onClose = func() error {
		return command.Process.Signal(syscall.SIGTERM)
	}

	// TODO: read error/info stream, await for watches to establish
	return &watcher, errCommandStart
}
func (InotifyWatcher) Name() string {
	return "inotify"
}
func (watcher *InotifyWatcher) Close() error {
	return watcher.onClose()
}
func (watcher *InotifyWatcher) Modifications() <-chan Modification {
	return watcher.modifications
}
func (watcher *InotifyWatcher) getNrFiles(root string) (uint64, error) {
	var nrFiles uint64
	done := make(chan struct{}, 1)
	var errStopwatch error
	doNotWrite := cancellableTimer(
		1 * time.Second,
		func() {
			//fmt.Println("Calculating number of files in watched directory")
			errStopwatch = stopwatch(
				"Calculating number of files in watched directory",
				func() error {
					select { case <-done: return nil }
				},
			)
			if errStopwatch == nil {
				fmt.Printf("%d files must be watched in total\n", nrFiles)
			}
		},
	)
	command := exec.Command("find", root, "-type", "f") // MAYBE: `wc -l`
	out, err := command.StdoutPipe()
	if err != nil { return 0, err }
	buffer := bufio.NewScanner(out)
	err = command.Start()
	if err != nil { return 0, err }
	for buffer.Scan() { nrFiles++ }
	(*doNotWrite)()
	done <- struct{}{}
	if errStopwatch != nil { return 0, errStopwatch }
	return nrFiles, nil
}
func (watcher *InotifyWatcher) getMaxUserWatchers() (uint64, error) {
	var maxNrWatchers uint64
	var errParseUint, errCat error
	if !my.RunCommand(
		"",
		"cat /proc/sys/fs/inotify/max_user_watches",
		func(out string) {
			maxNrWatchers, errParseUint = strconv.ParseUint(out, 10, 64)
		},
		func(err string) {
			errCat = errors.New(err)
		},
	) {
		return 0, errors.New("could not determine max_user_watchers")
	}
	if errCat != nil { return 0, errCat }
	if errParseUint != nil { return 0, errParseUint }
	if maxNrWatchers == 0 { return 0, errors.New("could not determine max_user_watchers") }

	return maxNrWatchers, nil
}
func (watcher *InotifyWatcher) getRequiredNrWatchers(nrFiles uint64) uint64 {
	nrWatchers := uint64(1)
	for nrFiles > nrWatchers / 2 {
		nrWatchers *= 2
	}
	return nrWatchers
}
func (watcher *InotifyWatcher) setMaxUserWatchers(nrWatchers uint64) error {
	args := []string{
		"sysctl",
		"fs.inotify.max_user_watches=" + strconv.FormatUint(nrWatchers, 10),
	}
	fmt.Println("Higher number of max_user_watchers required")
	fmt.Println("sudo " + strings.Join(args, " "))
	command := exec.Command("sudo", args...)
	command.Stdin = os.Stdin

	return command.Run()
}
