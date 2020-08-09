package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/fsnotify/fsnotify"
	"io/ioutil"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

var files []string
var watcher *fsnotify.Watcher

// lockers
var syncing sync.WaitGroup
var waitingMaster CountableWaitGroup
var syncingQueued bool
type CountableWaitGroup struct {
	wg sync.WaitGroup
	count int
}
func (wg *CountableWaitGroup) Add(c int) {
	wg.count += c
	wg.wg.Add(c)
}
func (wg *CountableWaitGroup) DoneAll() {
	for wg.count > 0 { wg.Add(-1) }
}
func (wg *CountableWaitGroup) Wait() {
	wg.wg.Wait()
}

// parameters
var localDir string
var remoteHost string
var remoteDir string
// flags
var identityFile *string
var connTimeout int
var ignored *regexp.Regexp
var verbosity int

var controlPath string

func PanicIf(err error) {
	if err != nil {
		fmt.Println(err.Error())
		panic(err)
	}
}

func runCommand(dir string, cmd string, onStdout func(string), onStderr func(string)) bool {
	command := exec.Command("sh", "-c", cmd)
	command.Dir = dir

	stdout, err := command.StdoutPipe()
	PanicIf(err)
	stdoutScanner := bufio.NewScanner(stdout)
	go func() {
		for stdoutScanner.Scan() {
			stdout := stdoutScanner.Text()
			fmt.Println(stdout)
			if onStdout != nil { onStdout(stdout) }
		}
	}()

	stderr, err := command.StderrPipe()
	PanicIf(err)
	stderrScanner := bufio.NewScanner(stderr)
	go func() {
		for stderrScanner.Scan() {
			stderr := stderrScanner.Text()
			writeToStderr(stderr)
			if onStderr != nil { onStderr(stderr) }
		}
	}()

	return command.Run() == nil
}

func writeToStderr(text string) {
	_, err := fmt.Fprintln(os.Stderr, text)
	PanicIf(err)
}

func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	return !os.IsNotExist(err)
}

func syncFiles(sshCmd string) {
	if syncingQueued { return }
	syncingQueued = true

	waitingMaster.Wait()
	syncing.Wait()

	syncingQueued = false

	if len(files) == 0 { return }

	syncing.Add(1)

	filesUnique := make(map[string]interface{})
	for _, file := range files { filesUnique[file] = nil }
	files = make([]string, 0)

	existing := make([]string, 0)
	deleted := make([]string, 0)
	for file := range filesUnique {
		if fileExists(localDir + "/" + file) {
			existing = append(existing, file)
		} else {
			deleted = append(deleted, file)
		}
	}

	commands := make(map[string]string)
	if len(existing) > 0 {
		commands[fmt.Sprintf("uploading %d file(s)", len(existing))] = fmt.Sprintf(
			"rsync -azER -e '%s' %s %s:%s > /dev/null",
			sshCmd,
			strings.Join(existing, " "),
			remoteHost,
			remoteDir,
		)
	}
	if len(deleted) > 0 {
		commands[fmt.Sprintf("deleting %d file(s)", len(deleted))] = fmt.Sprintf(
			"%s %s 'cd %s && rm -rf %s'",
			sshCmd,
			remoteHost,
			remoteDir,
			strings.Join(deleted, " "),
		)
	}

	success := true
	if verbosity == 0 || verbosity == 1 {
		commands2 := make([]string, 0, len(commands))
		for _, command := range commands { commands2 = append(commands2, command) }
		operation := func() bool {
			return runCommand(
				localDir,
				strings.Join(commands2, " && "),
				nil,
				nil,
			)
		}
		if verbosity == 0 {
			success = operation()
		} else {
			description := make([]string, 0)
			for symbol, nr := range map[string]int{
				"+": len(existing),
				"-": len(deleted),
			} {
				if nr > 0 { description = append(description, fmt.Sprintf("%s%d", symbol, nr)) }
			}
			success = stopwatch(strings.Join(description, " "), operation)
		}
	} else if verbosity == 2 || verbosity == 3 {
		for description, command := range commands {
			if verbosity == 3 { description = fmt.Sprintf("%s: %s", description, command) }
			success = success && stopwatch(
				description,
				func() bool {
					return runCommand(
						localDir,
						command,
						nil,
						nil,
					)
				},
			)
			if !success { break }
		}
	}

	if !success {
		files = append(files, existing...)
		files = append(files, deleted...)
		go syncFiles(sshCmd)
	}

	syncing.Done()
}

func watchDirRecursive(path string, processor func(fsnotify.Event)) {
	var err error
	watcher, err = fsnotify.NewWatcher()
	PanicIf(err)
	defer func() { PanicIf(watcher.Close()) }()

	PanicIf(filepath.Walk(
		path,
		func(path string, fi os.FileInfo, err error) error {
			PanicIf(err)
			if fi.Mode().IsDir() { return watcher.Add(path) }
			return nil
		},
	))

	for {
		select {
			case event := <-watcher.Events: processor(event)
			case err := <-watcher.Errors: PanicIf(err)
		}
	}
}

func stripTrailSlash(path string) string {
	last := len(path) - 1
	if (len(path) > 0) && (path[last:] == "/" || path[last:] == "\\") { path = path[:last] }
	return path
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

func parseArguments() {
	identityFile     = flag.String("i", "", "identity file (rsa)")
	connTimeoutFlag := flag.Int("t", 5, "connection timeout (seconds)")
	verbosityFlag   := flag.Int("v", 2, "verbosity level (0-3)")
	exclude         := flag.String("e", "", "exclude pattern (regexp, f.e. '^\\.git/')")

	flag.Parse()

	if flag.NArg() != 3 {
		writeToStderr("Usage: of " + os.Args[0] + ":\nOptional flags:")
		flag.PrintDefaults()
		writeToStderr(
			"Required parameters:\n" +
				"  SOURCE - local directory (absolute path)\n" +
				"  HOST (IP or HOST or USER@HOST)\n" +
				"  DESTINATION - remote directory (absolute path)]",
		)
		os.Exit(1)
	}

	connTimeout = *connTimeoutFlag
	verbosity   = *verbosityFlag
	if exclude != nil {
		var err error
		ignored, err = regexp.Compile(*exclude)
		PanicIf(err)
	}

	localDir   = stripTrailSlash(flag.Arg(0))
	remoteHost = flag.Arg(1)
	remoteDir  = stripTrailSlash(flag.Arg(2))

	if localDir[:2] == "~/" {
		usr, err := user.Current()
		PanicIf(err)
		dir := usr.HomeDir
		localDir = filepath.Join(dir, localDir[2:])
	}
}

func masterConnection(sshCmd string) {
	closeMaster := func() {
		runCommand(
			localDir,
			fmt.Sprintf("%s -O exit %s 2>/dev/null", sshCmd, remoteHost),
			nil,
			nil,
		)
	}

	exit := make(chan os.Signal)
	signal.Notify(exit, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-exit
		closeMaster()
		_ = os.Remove(controlPath)
		os.Exit(0)
	}()

	waitingMaster.Add(1)
	closeMaster()
	for {
		fmt.Print("Establishing SSH Master connection... ")
		runCommand(
			localDir,
			fmt.Sprintf(
				"%s -o ServerAliveInterval=%d -o ServerAliveCountMax=1 -M %s 'echo done && sleep infinity'",
				sshCmd,
				connTimeout,
				remoteHost,
			),
			func(string) { waitingMaster.DoneAll() },
			nil,
		)
		closeMaster()
		waitingMaster.Add(1)
		time.Sleep(time.Duration(connTimeout) * time.Second)
	}
}

func main() {
	parseArguments()

	controlPathFile, err := ioutil.TempFile("", "sshmirror-")
	PanicIf(err)
	controlPath = controlPathFile.Name()
	PanicIf(os.Remove(controlPath))

	sshCmd := fmt.Sprintf(
		"ssh -o ControlMaster=auto -o ControlPath=%s -o ConnectTimeout=%d -o ConnectionAttempts=1",
		controlPath,
		connTimeout,
	)
	if identityFile != nil { sshCmd += " -i " + *identityFile }

	go masterConnection(sshCmd)

	var cancelFirst *context.CancelFunc
	var cancelLast *context.CancelFunc
	watchDirRecursive(
		localDir,
		func(event fsnotify.Event) {
			if event.Op == fsnotify.Chmod { return }

			filename := event.Name[len(localDir)+1:]

			if ignored != nil && ignored.MatchString(filename) { return }

			files = append(files, filename)

			doSync := func() {
				(*cancelFirst)()
				(*cancelLast)()
				cancelFirst = nil
				cancelLast = nil
				syncFiles(sshCmd)
			}

			if cancelFirst == nil {
				cancelFirst = cancellableTimer(
					5 * time.Second,
					doSync,
				)
			}

			if cancelLast != nil { (*cancelLast)() }
			cancelLast = cancellableTimer(
				500 * time.Millisecond,
				doSync,
			)
		},
	)
}
