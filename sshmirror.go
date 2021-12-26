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
var RunCommand = my.RunCommand
var PanicIf = my.PanicIf
var WriteToStderr = my.WriteToStderr

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

var ignored *regexp.Regexp // TODO: move to `Config`
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
	io.Closer
	Upload(filenames []string) bool
	Delete(filenames []string) bool
}

type sshClient struct {
	RemoteClient
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
	return client.runCommand(
		fmt.Sprintf(
			"%s %s 'cd %s && rm -rf %s'",
			client.sshCmd,
			client.config.remoteHost,
			client.config.remoteDir,
			strings.Join(filenames, " "),
		),
		nil,
	)
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
		if fileExists(localDir + "/" + file) {
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

func watchDirRecursive(path string, processor func(fsnotify.Event)) context.CancelFunc {
	watcher, err := fsnotify.NewWatcher()
	PanicIf(err)

	Must(filepath.Walk(
		path,
		func(path string, fi os.FileInfo, err error) error {
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
		if (len(path) > 0) && (path[last:] == "/" || path[last:] == "\\") { path = path[:last] }
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
	var files []string
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

		if len(files) == 0 { return }
		filesToSync := make([]string, len(files))
		copy(filesToSync, files)
		files = []string{}

		syncFiles(client, config.localDir, filesToSync)

		queueSize -= len(filesToSync)
		if queueSize == 0 { client.onReady() }
	}

	client.stopWatching = watchDirRecursive(
		config.localDir,
		func(event fsnotify.Event) {
			if event.Op == fsnotify.Chmod { return }
			filename := event.Name[len(config.localDir)+1:]
			if ignored != nil && ignored.MatchString(filename) { return }
			queueSize += 1
			files = append(files, filename)

			if cancelFirst == nil { cancelFirst = cancellableTimer(5 * time.Second, doSync) }
			if cancelLast != nil { (*cancelLast)() }
			cancelLast = cancellableTimer(500 * time.Millisecond, doSync)
		},
	)

	return client
}

func main() {
	launchClient(parseArguments())
	select {}
}
