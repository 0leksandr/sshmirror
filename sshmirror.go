package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/0leksandr/my.go"
	"io"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// TODO: upload directories on initial sync
// TODO: timeout sync operations
// TODO: re-sync with timeout on error
// TODO: ignore special types of files (pipes, block devices etc.)
// TODO: support directories
// TODO: support scp without rsync
// TODO: multiple files movements with one command (with preserved order)
// MAYBE: support symlinks
// MAYBE: automatically adjust timeout based on server response rate
// MAYBE: copy permissions
// MAYBE: copy files metadata (creation time etc.)
// MAYBE: upload new files with scp
// MAYBE: test that all interfaces are correctly implemented

var Must = my.Must
var PanicIf = my.PanicIf
var RunCommand = my.RunCommand
var WriteToStderr = my.WriteToStderr

type Locker struct {
	wg     sync.WaitGroup
	mutex  sync.Mutex
	locked bool
}
func (locker *Locker) Lock() {
	locker.mutex.Lock()
	if !locker.locked {
		locker.locked = true
		locker.wg.Add(1)
	}
	locker.mutex.Unlock()
}
func (locker *Locker) Unlock() {
	locker.mutex.Lock()
	if locker.locked {
		locker.locked = false
		locker.wg.Done()
	}
	locker.mutex.Unlock()
}
func (locker *Locker) Wait() {
	locker.wg.Wait()
}

type Filename string
func (filename Filename) Escaped() string {
	return wrapApostrophe(string(filename))
}
func (filename Filename) Real() string {
	return string(filename)
}

type Config struct {
	// parameters
	localDir   string
	remoteHost string
	remoteDir  string

	// flags
	identityFile string
	connTimeout  int
	verbosity    int
	exclude      string
	errorCmd     string
	watcher      string
}
func (Config) ParseArguments() Config {
	identityFile := flag.String("i", "", "identity file (rsa)")
	connTimeout  := flag.Int("t", 5, "connection timeout (seconds)")
	verbosity    := flag.Int("v", 2, "verbosity level (0-3)")
	exclude      := flag.String("e", "", "exclude pattern (regexp, f.e. '^\\.git/')")
	errorCmd     := flag.String(
		"error-cmd",
		"",
		"command, that will be called when errors occur. Text of an error will be passed to it as the last " +
			"argument",
	)
	watcher := flag.String(
		"watcher",
		"",
		fmt.Sprintf("FS watcher. Available values: %s, %s", InotifyWatcher{}.Name(), FsnotifyWatcher{}.Name()),
	)

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

	localDir   := stripTrailSlash(flag.Arg(0))
	remoteHost := flag.Arg(1)
	remoteDir  := stripTrailSlash(flag.Arg(2))

	if len(localDir) >= 2 && localDir[:2] == "~/" {
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
		verbosity:    *verbosity,
		exclude:      *exclude,
		errorCmd:     *errorCmd,
		watcher:      *watcher,
	}
}

type RemoteManager struct {
	io.Closer
	LoggerAware
	client    RemoteClient
	verbosity int // MAYBE: enum
	localDir  string
}
func (RemoteManager) New(config Config) RemoteManager {
	return RemoteManager{
		client:    sshClient{}.New(config),
		verbosity: config.verbosity,
		localDir:  config.localDir,
	}
}
func (manager RemoteManager) Close() error {
	return manager.client.Close()
}
func (manager RemoteManager) SetLogger(logger Logger) {
	manager.client.SetLogger(logger)
}
func (manager RemoteManager) Ready() *Locker {
	return manager.client.Ready()
}
func (manager RemoteManager) Sync(queue UploadingModificationsQueue) error {
	doSync := func(message string, operation func() error) error {
		if message == "" {
			return operation()
		} else {
			return stopwatch(message, operation)
		}
	}

	if len(queue.deleted) > 0 {
		deletedFilenames := make([]Filename, 0, len(queue.deleted))
		for _, deleted := range queue.deleted { deletedFilenames = append(deletedFilenames, deleted.filename) }
		if err := doSync(
			manager.message(deletedFilenames, "-", "deleting"),
			func() error { return manager.client.Delete(deletedFilenames) },
		); err != nil { return err }
	}

	if len(queue.moved) > 0 {
		movedFilenames := make([]Filename, 0, len(queue.moved))
		for _, moved := range queue.moved { movedFilenames = append(movedFilenames, moved.from) }
		if err := doSync(
			manager.message(movedFilenames, "^", "moving"),
			func() error {
				for _, moved := range queue.moved {
					if err := manager.client.Move(moved.from, moved.to); err != nil { return err }
				}
				return nil
			},
		); err != nil { return err }
	}

	if len(queue.updated) > 0 {
		updatedFilenames := make([]Filename, 0, len(queue.updated))
		for _, updated := range queue.updated { updatedFilenames = append(updatedFilenames, updated.filename) }
		if err := doSync(
			manager.message(updatedFilenames, "+", "uploading"),
			func() error { return manager.client.Upload(updatedFilenames) },
		); err != nil { return err }
	}

	return nil
}
func (manager RemoteManager) Fallback(queue UploadingModificationsQueue) { // MAYBE: legacy, remove
	var files []Filename
	for _, updated := range queue.updated { files = append(files, updated.filename) }
	for _, deleted := range queue.deleted { files = append(files, deleted.filename) }
	for _, moved := range queue.moved {
		files = append(files, moved.from)
		files = append(files, moved.to)
	}

	manager.fallbackFiles(files)
}
func (manager RemoteManager) fallbackFiles(files []Filename) {
	client := manager.client
	verbosity := manager.verbosity
	filesUnique := make(map[Filename]interface{})
	for _, file := range files { filesUnique[file] = nil }

	fileExists := func(filename Filename) bool {
		_, err := os.Stat(filename.Real())
		return !os.IsNotExist(err)
	}
	existing := make([]Filename, 0)
	deleted := make([]Filename, 0)
	for file := range filesUnique {
		if fileExists(Filename(manager.localDir + string(os.PathSeparator)) + file) {
			existing = append(existing, file)
		} else {
			deleted = append(deleted, file)
		}
	}

	result := true
	if verbosity == 0 {
		if len(existing) > 0 { result = result && (client.Upload(existing) == nil) }
		if len(deleted) > 0 { result = result && (client.Delete(deleted) == nil) }
	} else {
		if len(existing) > 0 {
			var uploadMessage string
			if verbosity == 1 {
				uploadMessage = fmt.Sprintf("+%d", len(existing))
			} else {
				uploadMessage = fmt.Sprintf("uploading %d file(s)", len(existing))
				if verbosity == 3 {
					existingStr := make([]string, 0, len(existing))
					for _, e := range existing { existingStr = append(existingStr, e.Real()) }
					uploadMessage = fmt.Sprintf("%s: %s", uploadMessage, strings.Join(existingStr, " "))
				}
			}
			result = stopwatch(
				uploadMessage,
				func() error { return client.Upload(existing) },
			) == nil
		}

		if result && len(deleted) > 0 {
			var uploadMessage string
			if verbosity == 1 {
				uploadMessage = fmt.Sprintf("-%d", len(deleted))
			} else {
				uploadMessage = fmt.Sprintf("deleting %d file(s)", len(deleted))
				if verbosity == 3 {
					deletedStr := make([]string, 0, len(deleted))
					for _, d := range deleted { deletedStr = append(deletedStr, d.Real()) }
					uploadMessage = fmt.Sprintf("%s: %s", uploadMessage, strings.Join(deletedStr, " "))
				}
			}
			result = stopwatch(
				uploadMessage,
				func() error { return client.Delete(deleted) },
			) == nil
		}
	}

	if !result {
		manager.fallbackFiles(files)
	}
}
func (manager RemoteManager) message(filenames []Filename, sign string, action string) string {
	if manager.verbosity == 0 { return "" }
	if manager.verbosity == 1 { return fmt.Sprintf("%s%d", sign, len(filenames)) }

	message := fmt.Sprintf("%s %d file", action, len(filenames))
	if len(filenames) > 1 { message += "s" }
	if manager.verbosity == 2 {
		return message
	} else {
		filenamesStrings := make([]string, 0, len(filenames))
		for _, filename := range filenames {
			filenamesStrings = append(filenamesStrings, filename.Real())
		}
		return message + ": " + strings.Join(filenamesStrings, " ")
	}
}

func stopwatch(description string, operation func() error) error {
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

	err := operation()
	(*stopTicking)()
	if err == nil { fmt.Println(" done in " + time.Since(start).String()) }
	return err
}
func cancellableTimer(timeout time.Duration, callback func()) *context.CancelFunc {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	go func() {
		<-ctx.Done()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) { callback() }
	}()
	return &cancel
}
func stripTrailSlash(path string) string {
	last := len(path) - 1
	if (len(path) > 0) && (path[last:] == string(os.PathSeparator) || path[last:] == "\\") { path = path[:last] }
	return path
}
func escapeApostrophe(text string) string {
	//text = strings.Replace(text, "\\", "\\\\", -1)
	//text = strings.Replace(text, "'", "\\'", -1)
	//return text

	//return strings.Join(strings.Split(text, "'"), `'"'"'`)
	return strings.Replace(text, "'", `'"'"'`, -1)
}
func wrapApostrophe(text string) string {
	return fmt.Sprintf("'%s'", escapeApostrophe(text))
}

type SSHMirror struct {
	io.Closer
	LoggerAware
	root    string
	watcher Watcher
	remote  RemoteManager
	logger  Logger
	onReady func() // only for test
}
func (SSHMirror) New(config Config) *SSHMirror {
	errorLogger := func() ErrorLogger {
		if config.errorCmd != "" {
			return ErrorCmdLogger{errorCmd: config.errorCmd}
		} else {
			return StdErrLogger{}
		}
	}()
	logger := NullLogger{errorLogger: errorLogger}

	watcher := (func() Watcher {
		switch config.watcher {
			case InotifyWatcher{}.Name():
				inotify, err := InotifyWatcher{}.New(config.localDir, config.exclude, logger)
				PanicIf(err)
				return inotify
			case FsnotifyWatcher{}.Name():
				return FsnotifyWatcher{}.New(config.localDir, config.exclude)
			default:
				if inotify, err := (InotifyWatcher{}.New(config.localDir, config.exclude, logger)); err == nil {
					return inotify
				} else {
					Must(inotify.Close())
					logger.Error(err.Error())
					logger.Error(
						"Warning! Current FS events provider: fsnotify. It has known problem of not tracking " +
							"contents of subdirectories, created after program was started. It can only reliably " +
							"track files in existing subdirectories",
					)
					return FsnotifyWatcher{}.New(config.localDir, config.exclude)
				}
		}
	})()

	remoteManager := RemoteManager{}.New(config)
	remoteManager.SetLogger(logger)

	return &SSHMirror{
		root:    config.localDir,
		watcher: watcher,
		remote:  remoteManager,
		logger:  logger,
		onReady: func() {},
	}
}
func (client *SSHMirror) Close() error {
	err1 := client.watcher.Close()
	err2 := client.remote.Close()

	if err1 != nil { return err1 }
	if err2 != nil { return err2 }
	return nil
}
func (client *SSHMirror) SetLogger(logger Logger) {
	client.logger = logger
	client.remote.SetLogger(logger)
}
func (client *SSHMirror) Run() {
	queue := ModificationsQueue{}
	var syncing sync.Mutex
	var cancelFirst *context.CancelFunc
	var cancelLast *context.CancelFunc

	exit := make(chan os.Signal)
	signal.Notify(exit, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-exit
		Must(client.Close())
		os.Exit(0)
	}()

	client.remote.Ready().Wait()
	client.logger.Debug("remote client initialized")

	doSync := func() {
		client.logger.Debug("doSync")
		syncing.Lock()
		defer syncing.Unlock()

		client.logger.Debug("waiting for remote client")
		client.remote.Ready().Wait()
		client.logger.Debug("remote client ready")

		if cancelFirst != nil {
			(*cancelFirst)()
			cancelFirst = nil
		}
		if cancelLast != nil {
			(*cancelLast)()
			cancelLast = nil
		}

		if queue.IsEmpty() { // during upload, multiple syncs were produces, first sync synchronized everything
			return
		}

		var doSync2 func(UploadingModificationsQueue, error) // TODO: rename
		doSync2 = func(prevUpload UploadingModificationsQueue, prevErr error) {
			client.logger.Debug("doSync2.queue", &queue)
			uploadingModificationsQueue, err := queue.Flush(client.root)
			PanicIf(err) // MAYBE: fallback
			if !uploadingModificationsQueue.Equals(prevUpload) {
				client.logger.Debug("uploadingModificationsQueue", uploadingModificationsQueue)
				if err2 := client.remote.Sync(uploadingModificationsQueue); err2 != nil {
					doSync2(uploadingModificationsQueue, err2)
				}
			} else {
				client.logger.Error(prevErr.Error())
				client.logger.Error("Applying fallback algorithm")
				client.remote.Fallback(uploadingModificationsQueue)
			}
		}
		doSync2(UploadingModificationsQueue{}, nil)

		if queue.IsEmpty() { client.onReady() }
	}

	modificationReceived := func(modification Modification) {
		client.logger.Debug("modification received", modification)
		if err := queue.AtomicAdd(modification); err == nil {
			if cancelFirst == nil { cancelFirst = cancellableTimer(5 * time.Second, doSync) }
			if cancelLast != nil { (*cancelLast)() }
			cancelLast = cancellableTimer(500 * time.Millisecond, doSync)
		} else {
			client.logger.Error(err.Error())
			// TODO: fallback
		}
	}

	select {
		case modification, ok := <-client.watcher.Modifications():
			if !ok { panic("modifications channel closed") }
			modificationReceived(modification)
		default:
			client.onReady()
	}

	for {
		select {
			case modification, ok := <-client.watcher.Modifications():
				if !ok { return } // TODO: make sure previous (running) modifications are uploaded
				modificationReceived(modification)
		}
	}
}

func main() {
	SSHMirror{}.New(Config{}.ParseArguments()).Run()
}
