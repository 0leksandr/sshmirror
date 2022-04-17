package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/0leksandr/my.go"
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

// TODO: upload directories on initial sync
// TODO: re-sync timeout
// TODO: ignore special types of files (pipes, block devices etc.)
// TODO: support symlinks
// TODO: support directories
// TODO: support scp without rsync
// MAYBE: use `inotifywait` instead of `fsnotify`, and get rid of movement troubles
// MAYBE: automatically adjust timeout based on server response rate
// MAYBE: move multiple at once with one command (but preserve order of "chained" movements)
// MAYBE: copy permissions
// MAYBE: copy files metadata (creation time etc.)
// MAYBE: upload new files with scp
// MAYBE: automatically trust SSH signatures for new hosts
// MAYBE: support ":" in filenames

var Must = my.Must
var PanicIf = my.PanicIf
var RunCommand = my.RunCommand
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

type sshClient struct { // TODO: rename
	RemoteClient
	io.Closer
	listener      *Listener // MAYBE: store it (and controlPath) in lambdas, that should be called in `Close`
	config        Config
	sshCmd        string
	controlPath   string
	waitingMaster *CountableWaitGroup
	done          bool // TODO: rename
	onReady       func() // just for test
	logger        LoggerInterface
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
		logger:        NullLogger{},
	}

	client.waitingMaster.Add(1)
	go client.keepMasterConnection()

	return client
}
func (client *sshClient) Close() error {
	client.done = true
	var err error
	if client.listener != nil { err = (*client.listener).Close() }
	client.closeMaster()
	_ = os.Remove(client.controlPath)
	return err
}
func (client *sshClient) Upload(filenames []string) bool {
	return client.runCommand(
		fmt.Sprintf(
			"rsync -azER -e '%s' -- %s %s:%s > /dev/null",
			client.sshCmd,
			strings.Join(escapeFilenames(filenames), " "),
			client.config.remoteHost,
			client.config.remoteDir,
		),
		nil,
	)
}
func (client *sshClient) Delete(filenames []string) bool {
	return client.runRemoteCommand(fmt.Sprintf(
		"rm -rf -- %s", // MAYBE: something more reliable
		strings.Join(escapeFilenames(filenames), " "),
	))
}
func (client *sshClient) Move(from string, to string) bool {
	return client.runRemoteCommand(fmt.Sprintf(
		"mv -- %s %s",
		wrapApostrophe(from),
		wrapApostrophe(to),
	))
}
func (client *sshClient) keepMasterConnection() {
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
			func(out string) {
				fmt.Println(out)
				client.logger.Debug("master ready")
				client.waitingMaster.DoneAll() // MAYBE: ensure this happens only once
			},
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
	client.logger.Debug("running command", command)

	return RunCommand(
		client.config.localDir,
		command,
		onStdout,
		client.logger.Error,
	)
}
func (client *sshClient) runRemoteCommand(command string) bool {
	return client.runCommand(
		fmt.Sprintf(
			"%s %s 'cd %s && (%s)'",
			client.sshCmd,
			client.config.remoteHost,
			client.config.remoteDir,
			escapeApostrophe(command),
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
	escapeFile := wrapApostrophe
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
func escapeFilenames(filenames []string) []string {
	escapedFilenames := make([]string, 0, len(filenames))
	for _, filename := range filenames {
		escapedFilenames = append(escapedFilenames, wrapApostrophe(filename))
	}
	return escapedFilenames
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

func (client *sshClient) Run() {
	queue := ModificationsQueue{}
	var syncing sync.Mutex
	var queueMx sync.Mutex
	var cancelFirst *context.CancelFunc
	var cancelLast *context.CancelFunc

	exit := make(chan os.Signal)
	signal.Notify(exit, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-exit
		Must(client.Close())
		os.Exit(0)
	}()

	listener := FsnotifyListener{}.New(client.config.localDir, ignored)
	client.listener = &listener

	client.waitingMaster.Wait()
	client.logger.Debug("master ready")

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

		if queue.IsEmpty() {
			client.logger.Error("Empty queue")
			return
		}

		queueMx.Lock()
		if err := queue.Optimize(client.config.localDir); err == nil { // MAYBE: return optimized, and put mutex inside queue
			queueCopy := queue.Copy()
			queue = ModificationsQueue{}
			queueMx.Unlock()
			queueCopy.Apply(client) // TODO: swap
		} else {
			queueMx.Unlock()
			client.logger.Error(err.Error())
			client.logger.Error("Applying fallback algorithm")
			syncFiles(client, client.config.localDir, listener.Fallback())
		}

		if queue.IsEmpty() { client.onReady() }
	}

	modificationReceived := func(modification Modification) {
		client.logger.Debug("modification received", modification)
		queueMx.Lock()
		if err := queue.Add(modification); err == nil {
			if cancelFirst == nil { cancelFirst = cancellableTimer(5 * time.Second, doSync) }
			if cancelLast != nil { (*cancelLast)() }
			cancelLast = cancellableTimer(500 * time.Millisecond, doSync)
		} else {
			client.logger.Error(err.Error())
			// TODO: fallback
		}
		queueMx.Unlock()
	}

	select {
		case modification, ok := <-listener.Modifications():
			if !ok { panic("modifications channel closed") }
			modificationReceived(modification)
		default:
			client.onReady()
	}

	for {
		select {
			case modification, ok := <-listener.Modifications():
				if !ok { return } // TODO: make sure previous (running) modifications are uploaded
				modificationReceived(modification)
		}
	}
}

func main() {
	sshClient{}.New(parseArguments()).Run()
}
