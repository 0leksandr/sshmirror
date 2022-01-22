package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/0leksandr/my.go"
	"github.com/fsnotify/fsnotify"
	"io"
	"io/ioutil"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

// TODO: re-sync timeout

var Must = my.Must
var PanicIf = my.PanicIf
var RunCommand = my.RunCommand
var WriteToStderr = my.WriteToStderr

type Modification interface {
	Join(queue *ModificationsQueue) error
}
// problem with created+updated: impossible to determine which one of the two is a moved file. I.e. a file was moved
// to a new location. Is it created or updated?
// TODO: return `Created`, and improve created + instantly deleted cases

type Updated struct { // any file, that must be uploaded // MAYBE: Written
	filename string
}
type Deleted struct { // existing file, that was deleted
	filename string
}
type Moved struct { // existing file, that was moved to a new location
	from string
	to   string
}
func (updated Updated) Join(queue *ModificationsQueue) error {
	addToQueue := true
	for _, previouslyUpdated := range queue.updated {
		if previouslyUpdated.filename == updated.filename {
			addToQueue = false
		}
	}
	for i, moved := range queue.moved {
		if moved.to == updated.filename {
			queue.removeMoved(i)
			if !queue.HasModifications(moved.from) {
				if err := queue.Add(Deleted{filename: moved.from}); err != nil { return err }
			}
		}
	}
	for i, deleted := range queue.deleted {
		if deleted.filename == updated.filename {
			queue.removeDeleted(i)
		}
	}
	if addToQueue { queue.updated = append(queue.updated, updated) }
	return nil
}
func (deleted Deleted) Join(queue *ModificationsQueue) error {
	for i, updated := range queue.updated {
		if updated.filename == deleted.filename {
			queue.removeUpdated(i)
		}
	}
	for i, moved := range queue.moved {
		if moved.from == deleted.filename {
			return errors.New("moved from, then deleted")
		}
		if moved.to == deleted.filename {
			queue.removeMoved(i)
			if !queue.HasModifications(moved.from) {
				if err := queue.Add(Deleted{filename: moved.from}); err != nil { return err }
			}
		}
	}
	for _, previouslyDeleted := range queue.deleted {
		if previouslyDeleted.filename == deleted.filename {
			return errors.New("deleted, then deleted")
		}
	}
	queue.deleted = append(queue.deleted, deleted)
	return nil
}
func (moved Moved) Join(queue *ModificationsQueue) error {
	if moved.from == moved.to { return errors.New("moving to same location") }

	addToQueue := true
	for _, deleted := range queue.deleted { // before the next block, because else will be overwritten
		if deleted.filename == moved.from {
			return errors.New("deleted, then moved from")
		}
	}
	for i, updated := range queue.updated {
		if updated.filename == moved.from {
			if err := queue.Add(Deleted{filename: moved.from}); err != nil { return err }
			if err := queue.Add(Updated{filename: moved.to}); err != nil { return err }
			addToQueue = false
		}
		if updated.filename == moved.to {
			queue.removeUpdated(i)
		}
	}
	for i, previouslyMoved := range queue.moved {
		if previouslyMoved.from == moved.from {
			return errors.New("moved from, then moved from")
		}
		if previouslyMoved.from == moved.to {
			// TODO: think about
		}
		if previouslyMoved.to == moved.from {
			queue.moved[i].to = moved.to
			if err := queue.Add(Deleted{filename: moved.from}); err != nil { return err }
			addToQueue = false
		}
		if previouslyMoved.to == moved.to {
			queue.removeMoved(i)
			if !queue.HasModifications(previouslyMoved.from) {
				if err := queue.Add(Deleted{filename: previouslyMoved.from}); err != nil { return err }
			}
		}
	}
	if addToQueue { queue.moved = append(queue.moved, moved) }
	return nil
}

type ModificationsQueue struct {
	// MAYBE: modifications []Modification
	updated []Updated
	deleted []Deleted
	moved   []Moved
}
func (queue *ModificationsQueue) Add(modification Modification) error {
	return modification.Join(queue)
}
func (queue *ModificationsQueue) HasModifications(filename string) bool {
	for _, updated := range queue.updated {
		if updated.filename == filename { return true }
	}
	for _, deleted := range queue.deleted {
		if deleted.filename == filename { return true }
	}
	for _, moved := range queue.moved {
		if moved.to == filename { return true }
	}
	return false
}
func (queue *ModificationsQueue) Apply(client RemoteClient) {
	if len(queue.deleted) > 0 {
		deletedFilenames := make([]string, 0, len(queue.deleted))
		for _, deleted := range queue.deleted { deletedFilenames = append(deletedFilenames, deleted.filename) }
		client.Delete(deletedFilenames)
	}

	for _, moved := range queue.moved {
		client.Move(moved.from, moved.to)
	}

	if len(queue.updated) > 0 {
		updatedFilenames := make([]string, 0, len(queue.updated))
		for _, updated := range queue.updated { updatedFilenames = append(updatedFilenames, updated.filename) }
		client.Upload(updatedFilenames)
	}
}
func (queue *ModificationsQueue) removeUpdated(i int) {
	last := len(queue.updated) - 1
	if i != last { queue.updated[i] = queue.updated[last] }
	queue.updated = queue.updated[:last]
}
func (queue *ModificationsQueue) removeDeleted(i int) {
	last := len(queue.deleted) - 1
	if i != last { queue.deleted[i] = queue.deleted[last] }
	queue.deleted = queue.deleted[:last]
}
func (queue *ModificationsQueue) removeMoved(i int) {
	last := len(queue.moved) - 1
	if i != last { queue.moved[i] = queue.moved[last] }
	queue.moved = queue.moved[:last]
}

func readModifications(events []fsnotify.Event, localDir string) (*ModificationsQueue, error) {
	queue := ModificationsQueue{}
	for i := 0; i < len(events); i++ {
		event := events[i]
		switch event.Op {
			case fsnotify.Create, fsnotify.Write:
				err := queue.Add(Updated{filename: event.Name})
				if err != nil { return nil, err }
			case fsnotify.Remove:
				err := queue.Add(Deleted{filename: event.Name})
				if err != nil { return nil, err }
			case fsnotify.Rename:
				if i < len(events) - 1 {
					nextEvent := events[i + 1]
					if nextEvent.Op == fsnotify.Create { // TODO: check contents (checksums, modification times)
						err := queue.Add(Moved{
							from: event.Name,
							to:   nextEvent.Name,
						})
						if err != nil { return nil, err }
						i++
						break
					}
				}
				err := queue.Add(Deleted{filename: event.Name})
				if err != nil { return nil, err }
		}
	}

	// check for circular move
	removedCircular := true
	for removedCircular {
		var err error
		removedCircular, err = (func() (bool, error) {
			for i := 0; i < len(queue.moved); i++ {
				movedI := queue.moved[i]
				for j := i+1; j < len(queue.moved); j++ {
					movedJ := queue.moved[j]
					if movedI.from == movedJ.to && movedI.to == movedJ.from {
						queue.removeMoved(j)
						queue.removeMoved(i)
						// MAYBE: something smarter
						var err2 error
						for _, filename := range [2]string{movedI.from , movedJ.from} {
							if err3 := queue.Add(Updated{filename: filename}); err3 != nil { err2 = err3 }
						}
						return true, err2
					}
				}
			}
			return false, nil
		})()
		if err != nil { return nil, err }
	}

	// false moves
	fileEmpty := func(filename string) (bool, error) {
		fileInfo, err := os.Stat(localDir + string(os.PathSeparator) + filename)
		return fileInfo.Size() == 0, err
	}
	for i := 0; i < len(queue.moved); i++ {
		moved := queue.moved[i]
		empty, err := fileEmpty(moved.to)
		if err != nil { return nil, err }
		if empty {
			queue.removeMoved(i)
			i--
			if err2 := queue.Add(Updated{filename: moved.to}); err2 != nil { return nil, err2 }
			if !queue.HasModifications(moved.from) {
				if err2 := queue.Add(Deleted{filename: moved.from}); err2 != nil { return nil, err2 }
			}
		}
	}

	return &queue, nil
}

type CountableWaitGroup struct {
	wg    sync.WaitGroup
	count int
}
func (wg *CountableWaitGroup) Add(c int) {
	wg.count += c
	wg.wg.Add(c)
}
func (wg *CountableWaitGroup) DoneAll() {
	//for wg.count > 0 { wg.Add(-1) }
	wg.Add(-wg.count)
}
func (wg *CountableWaitGroup) Wait() {
	wg.wg.Wait()
}

var ignored *regexp.Regexp // MAYBE: move to `Config`
var verbosity int

type Config struct {
	// parameters
	localDir   string
	remoteHost string
	remoteDir  string

	// flags
	identityFile string
	connTimeout  int
}

type RemoteClient interface {
	Upload(filenames []string) bool // TODO: return error
	Delete(filenames []string) bool
	Move(from string, to string) bool
}

type sshClient struct {
	RemoteClient
	io.Closer
	config        Config
	sshCmd        string
	controlPath   string
	waitingMaster *CountableWaitGroup
	done          bool // TODO: rename
	stopWatching  func()
	onReady       func() // just for test
}
func (sshClient) New(config Config) *sshClient {
	controlPathFile, err := ioutil.TempFile("", "sshmirror-")
	PanicIf(err)
	controlPath := controlPathFile.Name()
	Must(os.Remove(controlPath))

	sshCmd := fmt.Sprintf(
		"ssh -o ControlMaster=auto -o ControlPath=%s -o ConnectTimeout=%d -o ConnectionAttempts=1",
		controlPath,
		config.connTimeout,
	)
	if config.identityFile != "" { sshCmd += " -i " + config.identityFile }

	var waitingMaster CountableWaitGroup

	client := &sshClient{
		config:        config,
		sshCmd:        sshCmd,
		controlPath:   controlPath,
		waitingMaster: &waitingMaster,
		onReady:       func() {},
	}

	go client.keepMasterConnection()

	return client
}
func (client *sshClient) Close() error {
	client.done = true
	client.stopWatching()
	client.closeMaster()
	_ = os.Remove(client.controlPath)
	return nil
}
func (client *sshClient) Upload(filenames []string) bool {
	return client.runCommand(
		fmt.Sprintf(
			"rsync -azER -e '%s' %s %s:%s > /dev/null",
			client.sshCmd,
			strings.Join(filenames, " "),
			client.config.remoteHost,
			client.config.remoteDir,
		),
		nil,
	)
}
func (client *sshClient) Delete(filenames []string) bool {
	return client.runRemoteCommand(fmt.Sprintf(
		"rm -rf %s", // TODO: check flags
		strings.Join(filenames, " "),
	))
}
func (client *sshClient) Move(from string, to string) bool {
	return client.runRemoteCommand(fmt.Sprintf(
		"mv %s %s",
		from,
		to,
	))
}
func (client *sshClient) keepMasterConnection() {
	client.waitingMaster.Add(1)
	client.closeMaster()
	for {
		fmt.Print("Establishing SSH Master connection... ")

		client.runCommand(
			fmt.Sprintf(
				"%s -o ServerAliveInterval=%d -o ServerAliveCountMax=1 -M %s 'echo done && sleep infinity'",
				client.sshCmd,
				client.config.connTimeout,
				client.config.remoteHost,
			),
			func(string) { client.waitingMaster.DoneAll() },
		)

		client.closeMaster()
		if client.done { break }
		client.waitingMaster.Add(1)
		time.Sleep(time.Duration(client.config.connTimeout) * time.Second)
	}
}
func (client *sshClient) closeMaster() {
	client.runCommand(
		fmt.Sprintf("%s -O exit %s 2>/dev/null", client.sshCmd, client.config.remoteHost),
		nil,
	)
}
func (client *sshClient) runCommand(command string, onStdout func(string)) bool {
	return RunCommand(
		client.config.localDir,
		command,
		func(out string) {
			fmt.Println(out)
			onStdout(out)
		},
		WriteToStderr,
	)
}
func (client *sshClient) runRemoteCommand(command string) bool {
	return client.runCommand(
		fmt.Sprintf(
			"%s %s 'cd %s && %s'",
			client.sshCmd,
			client.config.remoteHost,
			client.config.remoteDir,
			command, // TODO: escape
		),
		nil,
	)
}

func syncFiles(client RemoteClient, localDir string, files []string) {
	filesUnique := make(map[string]interface{})
	for _, file := range files { filesUnique[file] = nil }

	fileExists := func(filename string) bool {
		_, err := os.Stat(filename)
		return !os.IsNotExist(err)
	}
	escapeFile := func(file string) string {
		return fmt.Sprintf("'%s'", file) // TODO: escape "'", "\" and special symbols
	}
	existing := make([]string, 0)
	deleted := make([]string, 0)
	for file := range filesUnique {
		if fileExists(localDir + string(os.PathSeparator) + file) {
			existing = append(existing, escapeFile(file))
		} else {
			deleted = append(deleted, escapeFile(file))
		}
	}

	result := true
	if verbosity == 0 {
		if len(existing) > 0 { result = result && client.Upload(existing) }
		if len(deleted) > 0 { result = result && client.Delete(deleted) }
	} else {
		if len(existing) > 0 {
			var uploadMessage string
			if verbosity == 1 {
				uploadMessage = fmt.Sprintf("+%d", len(existing))
			} else {
				uploadMessage = fmt.Sprintf("uploading %d file(s)", len(existing))
				if verbosity == 3 {
					uploadMessage = fmt.Sprintf("%s: %s", uploadMessage, strings.Join(existing, " "))
				}
			}
			result = stopwatch(
				uploadMessage,
				func() bool { return client.Upload(existing) },
			)
		}

		if result && len(deleted) > 0 {
			var uploadMessage string
			if verbosity == 1 {
				uploadMessage = fmt.Sprintf("-%d", len(deleted))
			} else {
				uploadMessage = fmt.Sprintf("deleting %d file(s)", len(deleted))
				if verbosity == 3 {
					uploadMessage = fmt.Sprintf("%s: %s", uploadMessage, strings.Join(deleted, " "))
				}
			}
			result = stopwatch(
				uploadMessage,
				func() bool { return client.Delete(deleted) },
			)
		}
	}

	if !result {
		syncFiles(client, localDir, files)
	}
}

func watchDirRecursive(path string, ignored *regexp.Regexp, processor func(fsnotify.Event)) context.CancelFunc {
	watcher, err := fsnotify.NewWatcher()
	PanicIf(err)

	isIgnored := func(path2 string) bool {
		if ignored == nil { return false }
		if path2[:len(path)] != path {
			panic(fmt.Sprintf("Unexpected local path: %s", path2))
		}
		path2 = path2[len(path):]
		if path2 != "" {
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

	stop := make(chan bool)

	go func() {
		for {
			select {
				case event := <-watcher.Events: processor(event)
				case err := <-watcher.Errors: PanicIf(err)
				case <-stop: return
			}
		}
	}()

	return func() {
		stop <- true
		close(stop)
		Must(watcher.Close())
	}
}

func stopwatch(description string, operation func() bool) bool {
	fmt.Print(description)
	start := time.Now()
	var stopTicking *context.CancelFunc
	var tick func()
	tick = func() {
		stopTicking = cancellableTimer(
			1 * time.Second,
			func() {
				fmt.Print(".")
				tick()
			},
		)
	}
	tick()

	result := operation()
	(*stopTicking)()
	if result { fmt.Println(" done in " + time.Since(start).String()) }
	return result
}

func cancellableTimer(timeout time.Duration, callback func()) *context.CancelFunc {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	go func() {
		<-ctx.Done()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) { callback() }
	}()
	return &cancel
}

func parseArguments() Config {
	identityFile  := flag.String("i", "", "identity file (rsa)")
	connTimeout   := flag.Int("t", 5, "connection timeout (seconds)")
	verbosityFlag := flag.Int("v", 2, "verbosity level (0-3)")
	exclude       := flag.String("e", "", "exclude pattern (regexp, f.e. '^\\.git/')")

	flag.Parse()

	if flag.NArg() != 3 {
		WriteToStderr("Usage: of " + os.Args[0] + ":\nOptional flags:")
		flag.PrintDefaults()
		WriteToStderr(
			"Required parameters:\n" +
				"  SOURCE - local directory (absolute path)\n" +
				"  HOST (IP or HOST or USER@HOST)\n" +
				"  DESTINATION - remote directory (absolute path)]",
		)
		os.Exit(1)
	}

	verbosity = *verbosityFlag
	if *exclude != "" {
		var err error
		ignored, err = regexp.Compile(*exclude)
		PanicIf(err)
	}

	stripTrailSlash := func(path string) string {
		last := len(path) - 1
		if (len(path) > 0) && (path[last:] == string(os.PathSeparator) || path[last:] == "\\") { path = path[:last] }
		return path
	}
	localDir   := stripTrailSlash(flag.Arg(0))
	remoteHost := flag.Arg(1)
	remoteDir  := stripTrailSlash(flag.Arg(2))

	if localDir[:2] == "~/" {
		usr, err := user.Current()
		PanicIf(err)
		dir := usr.HomeDir
		localDir = filepath.Join(dir, localDir[2:])
	}

	return Config{
		localDir:     localDir,
		remoteHost:   remoteHost,
		remoteDir:    remoteDir,
		identityFile: *identityFile,
		connTimeout:  *connTimeout,
	}
}

func launchClient(config Config) *sshClient { // TODO: move to `Client.New`
	client := sshClient{}.New(config)
	var events []fsnotify.Event
	var syncing sync.Mutex
	var queueSize int
	var cancelFirst *context.CancelFunc
	var cancelLast *context.CancelFunc

	exit := make(chan os.Signal)
	signal.Notify(exit, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-exit
		Must(client.Close())
		os.Exit(0)
	}()

	doSync := func() {
		syncing.Lock()
		defer syncing.Unlock()

		client.waitingMaster.Wait()

		if cancelFirst != nil {
			(*cancelFirst)()
			cancelFirst = nil
		}
		if cancelLast != nil {
			(*cancelLast)()
			cancelLast = nil
		}

		if len(events) == 0 { return }
		queueSize -= len(events) // TODO: atomic

		if queue, err := readModifications(events, config.localDir); err == nil {
			events = []fsnotify.Event{}
			queue.Apply(client)
		} else {
			my.WriteToStderr(err.Error())
			my.WriteToStderr("Applying fallback algorithm")
			filesToSync := make([]string, 0, len(events))
			for _, event := range events { filesToSync = append(filesToSync, event.Name) }
			events = []fsnotify.Event{}
			syncFiles(client, config.localDir, filesToSync)
		}

		if queueSize == 0 { client.onReady() }
	}

	client.stopWatching = watchDirRecursive(
		config.localDir,
		ignored,
		func(event fsnotify.Event) {
			if event.Op == fsnotify.Chmod { return }
			filename := event.Name[len(config.localDir)+1:]
			if ignored != nil && ignored.MatchString(filename) { return }
			queueSize += 1

			event.Name = filename
			events = append(events, event)

			if cancelFirst == nil { cancelFirst = cancellableTimer(5 * time.Second, doSync) }
			if cancelLast != nil { (*cancelLast)() }
			cancelLast = cancellableTimer(500 * time.Millisecond, doSync)
		},
	)

	client.waitingMaster.Wait()
	if queueSize == 0 { client.onReady() }

	return client
}

func main() {
	launchClient(parseArguments())
	select {}
}
