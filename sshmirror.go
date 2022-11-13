package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/0leksandr/my.go"
	"io"
	"io/fs"
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

// TODO: upload directories on initial sync
// TODO: timeout sync operations
// TODO: re-sync with timeout on error
// TODO: ignore special types of files (pipes, block devices etc.)
// TODO: support scp without rsync
// MAYBE: support symlinks
// MAYBE: automatically adjust timeout based on server response rate
// MAYBE: copy permissions
// MAYBE: copy files metadata (creation time etc.)
// MAYBE: upload new files with scp
// MAYBE: force rsync upload, disable --checksum

var Must = my.Must
var PanicIf = my.PanicIf
var StartCommand = my.StartCommand
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

type Filename string // TODO: rename. Pathname?
func (filename Filename) Escaped() string {
	return wrapApostrophe(string(filename))
}
func (filename Filename) Real() string {
	return string(filename)
}

type DummyFS struct { // TODO: remove?
	files []Path // TODO: optimize by sorting
}
func (dummyFS *DummyFS) AddFile(filename Filename) {
	// TODO: check for existence
	dummyFS.files = append(dummyFS.files, Path{}.New(filename))
}
func (dummyFS *DummyFS) Has(path Path) bool {
	for _, file := range dummyFS.files {
		if path.IsParentOf(file) { return true }
	}
	return false
}
func (dummyFS *DummyFS) Delete(path Path) {
	for i := 0; i < len(dummyFS.files); i++ {
		if path.IsParentOf(dummyFS.files[i]) {
			dummyFS.files = my.Remove(dummyFS.files, i).([]Path)
			i--
		}
	}
}
func (dummyFS *DummyFS) IsEmpty() bool {
	return len(dummyFS.files) == 0
}

type FileSize struct {
	megabytes uint64
	bytes     uint64
}
func (fileSize FileSize) Bytes() uint64 {
	return fileSize.megabytes * 1 << 20 + fileSize.bytes
}
func (fileSize FileSize) Add(other FileSize) FileSize {
	return FileSize{
		megabytes: fileSize.megabytes + other.megabytes,
		bytes:     fileSize.bytes + other.bytes,
	}
}
func (fileSize FileSize) IsLess(other FileSize) bool {
	return fileSize.Bytes() < other.Bytes()
}

type CancellableContext struct { // MAYBE: rename
	Result     func() error
	Cancel     func()
	ResultChan <-chan error // TODO: remove/handle
}

type SwitchChannelPaths struct { // MAYBE: atomic
	on bool
	ch chan Path
}
func (SwitchChannelPaths) New() *SwitchChannelPaths {
	return &SwitchChannelPaths{
		on: false,
		ch: make(chan Path), // MAYBE: buffer
	}
}
func (c *SwitchChannelPaths) On() {
	c.on = true
}
func (c *SwitchChannelPaths) Off() {
	c.on = false
	for {
		select {
			case _, ok := <-c.ch: if !ok { return }
			default: return
		}
	}
}
func (c *SwitchChannelPaths) Put(path Path) {
	if c.on { c.ch <- path }
}
func (c *SwitchChannelPaths) Get() <-chan Path {
	return c.ch
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
	watcher      string

	// services?
	logger Logger
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
		watcher:      *watcher,
		logger:       Logger{
			debug: NullLogger{},
			error: func() ErrorLogger {
				if *errorCmd != "" {
					return ErrorCmdLogger{errorCmd: *errorCmd}
				} else {
					return StdErrLogger{}
				}
			}(),
		},
	}
}

type RemoteManager struct {
	RemoteClient
	verbosity int // MAYBE: enum
	localDir  string
}
func (RemoteManager) New(config Config) RemoteManager {
	return RemoteManager{
		RemoteClient: sshClient{}.New(config),
		verbosity:    config.verbosity,
		localDir:     config.localDir,
	}
}
func (manager RemoteManager) Update(updated []Updated) CancellableContext {
	updatedFilenames := make([]Filename, 0, len(updated))
	for _, _updated := range updated { updatedFilenames = append(updatedFilenames, _updated.path.original) }
	cmdContext := manager.RemoteClient.Update(updated)
	cmdResult := make(chan error, 1)
	go func() {
		cmdResult <- manager.sync(
			manager.message(updatedFilenames, "+", "uploading"),
			cmdContext.Result,
		)
		close(cmdResult) // TODO: check if it's closed on cancel
	}()
	return CancellableContext{
		Result:     func() error { return <-cmdResult },
		Cancel:     cmdContext.Cancel,
		ResultChan: cmdResult,
	}
}
func (manager RemoteManager) InPlace(modifications []InPlaceModification) error {
	movedFilenames := make([]Filename, 0, len(modifications))
	for _, modification := range modifications {
		movedFilenames = append(movedFilenames, modification.OldFilename())
	}

	return manager.sync(
		manager.message(movedFilenames, "^", "(re)moving"),
		func() error { return manager.RemoteClient.InPlace(modifications) },
	)
}
func (manager RemoteManager) Ready() *Locker { // MAYBE: remove
	return manager.RemoteClient.Ready()
}
func (manager RemoteManager) Fallback(queue *ModificationsQueue) { // MAYBE: legacy, remove
	var files []Filename

	for _, inPlaceModification := range queue.inPlace {
		files = append(files, inPlaceModification.AffectedFiles()...)
	}
	queue.inPlace = make([]InPlaceModification, 0) // TODO: encapsulate

	for _, updated := range queue.GetUpdated(true) {
		files = append(files, updated.path.original)
	}

	manager.fallbackFiles(files)
}
func (manager RemoteManager) sync(message string, operation func() error) error {
	if message == "" {
		return operation()
	} else {
		return stopwatch(message, operation)
	}
}
func (manager RemoteManager) fallbackFiles(files []Filename) {
	verbosity := manager.verbosity
	filesUnique := make(map[Filename]interface{})
	for _, file := range files { filesUnique[file] = nil }

	fileExists := func(filename Filename) bool {
		_, err := os.Stat(filename.Real())
		return !os.IsNotExist(err)
	}
	updated := make([]Updated, 0)
	deleted := make([]InPlaceModification, 0)
	for file := range filesUnique {
		if fileExists(Filename(manager.localDir + string(os.PathSeparator)) + file) {
			updated = append(updated, Updated{Path{}.New(file)})
		} else {
			deleted = append(deleted, Deleted{Path{}.New(file)})
		}
	}

	result := true
	if verbosity == 0 {
		if len(updated) > 0 { result = result && (manager.RemoteClient.Update(updated).Result() == nil) }
		if len(deleted) > 0 { result = result && (manager.RemoteClient.InPlace(deleted) == nil) }
	} else {
		if len(updated) > 0 {
			var uploadMessage string
			if verbosity == 1 {
				uploadMessage = fmt.Sprintf("+%d", len(updated))
			} else {
				uploadMessage = fmt.Sprintf("uploading %d file(s)", len(updated))
				if verbosity == 3 {
					existingStr := make([]string, 0, len(updated))
					for _, u := range updated { existingStr = append(existingStr, u.path.original.Real()) }
					uploadMessage = fmt.Sprintf("%s: %s", uploadMessage, strings.Join(existingStr, " "))
				}
			}
			result = stopwatch(
				uploadMessage,
				func() error { return manager.RemoteClient.Update(updated).Result() },
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
					for _, d := range deleted { deletedStr = append(deletedStr, d.OldFilename().Real()) }
					uploadMessage = fmt.Sprintf("%s: %s", uploadMessage, strings.Join(deletedStr, " "))
				}
			}
			result = stopwatch(
				uploadMessage,
				func() error { return manager.RemoteClient.InPlace(deleted) },
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
		pathsStrings := make([]string, 0, len(filenames))
		for _, filename := range filenames {
			pathsStrings = append(pathsStrings, filename.Real())
		}
		return message + ": " + strings.Join(pathsStrings, " ")
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
	root    string // TODO: Filename
	watcher Watcher
	remote  RemoteManager
	logger  Logger
	syncing *Locker // only for test
}
func (SSHMirror) New(config Config) *SSHMirror {
	logger := config.logger
	var exclude *regexp.Regexp
	if config.exclude != "" { exclude = regexp.MustCompile(config.exclude) }

	watcher := (func() Watcher {
		switch config.watcher {
			case InotifyWatcher{}.Name():
				inotify, err := InotifyWatcher{}.New(config.localDir, exclude, logger)
				PanicIf(err)
				return inotify
			case FsnotifyWatcher{}.Name():
				return FsnotifyWatcher{}.New(config.localDir, exclude)
			default:
				if inotify, err := (InotifyWatcher{}.New(config.localDir, exclude, logger)); err == nil {
					return inotify
				} else {
					logger.Error(err.Error())
					logger.Error(
						"Warning! Current FS events provider: fsnotify. It has known problem of not tracking " +
							"contents of subdirectories, created after program was started. It can only reliably " +
							"track files in existing subdirectories",
					)
					return FsnotifyWatcher{}.New(config.localDir, exclude)
				}
		}
	})()

	return &SSHMirror{
		root:    config.localDir,
		watcher: watcher,
		remote:  RemoteManager{}.New(config),
		logger:  logger,
		syncing: &Locker{},
	}
}
func (client *SSHMirror) Close() error {
	err1 := client.watcher.Close()
	err2 := client.remote.Close()

	if err1 != nil { return err1 }
	if err2 != nil { return err2 }
	return nil
}
func (client *SSHMirror) Init(batchSize FileSize) error {
	// MAYBE: progress indicator

	synced := DummyFS{}

	modified := func(Path) {} // MAYBE: use `SwitchChannelPaths`

	go func() {
		for modification := range client.watcher.Modifications() {
			// PRIORITY: handle directories
			// MAYBE: something smarter
			for _, path := range modification.AffectedPaths() {
				modified(path)
				synced.Delete(path)
			}
		}
		panic("modifications channel closed") // THINK: some exit point
	}()

	upload := func(batch DummyFS) {
		modified = func(path Path) { batch.Delete(path) }
		defer func() { modified = func(Path) {} }()

		updated := make([]Updated, 0, len(batch.files))
		for _, filePath := range batch.files { updated = append(updated, Updated{filePath}) }

		if err := client.remote.Update(updated).Result(); err == nil {
			for _, filePath := range batch.files { synced.AddFile(filePath.original) }
		} else {
			client.logger.Error(err.Error())
		}
	}

	for {
		batch := DummyFS{}
		var curBatchSize FileSize
		errBatch := filepath.Walk( // MAYBE: optimize. Do not walk over `synced`
			client.root,
			func(path string, info fs.FileInfo, err error) error {
				if err != nil {
					client.logger.Error(err.Error())
					return nil
				}
				if info.IsDir() { return nil }
				filename := Filename(path)
				if synced.Has(Path{}.New(filename)) { return nil } // MAYBE: optimize
				batch.AddFile(filename)
				curBatchSize = curBatchSize.Add(FileSize{bytes: uint64(info.Size())})
				if curBatchSize.IsLess(batchSize) {
					return nil
				} else {
					return io.EOF
				}
			},
		)
		switch errBatch {
			case io.EOF:
				upload(batch)
			case nil:
				if batch.IsEmpty() {
					return nil
				} else {
					upload(batch)
				}
			default:
				return errBatch
		}
	}
}
func (client *SSHMirror) Run() {
	queue := TransactionalQueue{}.New()
	var syncing sync.Mutex
	var cancelFirst *context.CancelFunc
	var cancelLast *context.CancelFunc
	modifiedPaths := SwitchChannelPaths{}.New()

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
			client.logger.Debug("queue empty")
			client.syncing.Unlock() // for "empty" move - when file was moved outside and then into same location
			return
		}

		client.sync(queue, modifiedPaths)

		client.logger.Debug("queue after sync", queue)

		if queue.IsEmpty() { client.syncing.Unlock() }
	}

	modificationReceived := func(modification Modification) {
		client.logger.Debug("modification received", modification)
		client.syncing.Lock()
		queue.AtomicAdd(modification)
		if cancelFirst == nil { cancelFirst = cancellableTimer(5 * time.Second, doSync) }
		if cancelLast != nil { (*cancelLast)() }
		cancelLast = cancellableTimer(500 * time.Millisecond, doSync)
		for _, filename := range modification.AffectedPaths() { modifiedPaths.Put(filename) }
	}

	//select {
	//	case modification, ok := <-client.watcher.Modifications():
	//		if !ok { panic("modifications channel closed") }
	//		modificationReceived(modification)
	//	default:
	//		client.onReady()
	//}

	for {
		select {
			case modification, ok := <-client.watcher.Modifications():
				if !ok { return } // TODO: make sure previous (running) modifications are uploaded
				modificationReceived(modification)
		}
	}
}
func (client *SSHMirror) sync(queue *TransactionalQueue, modifiedPaths *SwitchChannelPaths) { // THINK: limit of tries
	client.logger.Debug("sync")

	for {
		client.logger.Debug("sync cycle")
		client.logger.Debug("queue", queue)

		queue.Begin()
		if inPlace := queue.GetInPlace(true); len(inPlace) > 0 {
			client.logger.Debug("inPlace", inPlace)
			if err := client.remote.InPlace(inPlace); err == nil {
				client.logger.Debug("success")
				queue.Commit()
			} else {
				client.logger.Debug("fail")
				client.logger.Error(err.Error())
				queue.Rollback()
			}

			continue // MAYBE: do not always sync all `InPlace` first; instead, prioritize them with `Updated`
		}
		queue.Commit()

		queue.Begin()
		if updated := queue.GetUpdated(true); len(updated) > 0 {
			client.logger.Debug("updated", updated)
			command := client.remote.Update(updated)
			modifiedPaths.On()

			uploading: for {
				select {
					case modifiedPath, ok := <-modifiedPaths.Get():
						if !ok { panic("modifiedPaths channel closed") }
						for _, _updated := range updated { // MAYBE: map
							if modifiedPath.Relates(_updated.path) {
								client.logger.Debug("cancelling upload. Modified path", modifiedPath)
								command.Cancel()
								queue.Rollback()
								break uploading
							}
						}
					case result := <-command.ResultChan:
						if result == nil {
							client.logger.Debug("success")
							queue.Commit()
						} else {
							client.logger.Debug("fail")
							client.logger.Error(result.Error())
							queue.Rollback()
						}
						break uploading
				}
			}
			modifiedPaths.Off()

			continue
		}
		queue.Commit()

		break
	}
}

func main() {
	SSHMirror{}.New(Config{}.ParseArguments()).Run()
}
