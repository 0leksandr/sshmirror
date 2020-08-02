package main

import (
	"bufio"
	"fmt"
	"github.com/fsnotify/fsnotify"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)
//import "../../go/src/my"

var files []string
var watcher *fsnotify.Watcher

var syncing bool
var masterConnectionAlive bool
var syncingAwait bool

func sshCommand(identityFile string, connTimeoutSec int) string {
	return fmt.Sprintf(
		"ssh -o ControlMaster=auto -o ControlPath=/tmp/ssh-%%r@%%h:%%p -o ConnectTimeout=%d -o ConnectionAttempts=1 -i %s",
		connTimeoutSec,
		identityFile,
	)
}

func runCommand(dir string, cmd string) bool {
	command := exec.Command("sh", "-c", cmd)
	command.Dir = dir

	stdout, err := command.StdoutPipe()
	PanicIf(err)
	stdoutScanner := bufio.NewScanner(stdout)
	go func() {
		for stdoutScanner.Scan() {
			fmt.Println(stdoutScanner.Text())
			masterConnectionAlive = true
		}
	}()

	stderr, err := command.StderrPipe()
	PanicIf(err)
	stderrScanner := bufio.NewScanner(stderr)
	go func() {
		for stderrScanner.Scan() {
			_, err := fmt.Fprintln(os.Stderr, stderrScanner.Text())
			PanicIf(err)
			masterConnectionAlive = false
		}
	}()

	return command.Run() == nil
}

func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	return !os.IsNotExist(err)
}

func syncFiles(localSource string, remoteHost string, remoteDestination string, sshCmd string) {
	if syncingAwait { return }
	syncingAwait = true

	// TODO: something better
	for !masterConnectionAlive { time.Sleep(100 * time.Millisecond) }
	for syncing { time.Sleep(100 * time.Millisecond) }

	syncingAwait = false

	if len(files) == 0 { return }

	syncing = true

	filesUnique := make(map[string]interface{})
	for _, file := range files { filesUnique[file] = nil }
	files = make([]string, 0)

	existing := make([]string, 0)
	deleted := make([]string, 0)
	for file := range filesUnique {
		if fileExists(localSource + "/" + file) {
			existing = append(existing, file)
		} else {
			deleted = append(deleted, file)
		}
	}

	if len(existing) > 0 {
		stopwatch(
			fmt.Sprintf("uploading %d file(s)", len(existing)),
			func() bool {
				return runCommand(
					localSource,
					fmt.Sprintf(
						"rsync -azER -e '%s' %s %s:%s > /dev/null",
						sshCmd,
						strings.Join(existing, " "),
						remoteHost,
						remoteDestination,
					),
				)
			},
		)
	}
	if len(deleted) > 0 {
		stopwatch(
			fmt.Sprintf("deleting %d file(s)", len(deleted)),
			func() bool {
				return runCommand(
					localSource,
					fmt.Sprintf(
						"%s %s 'cd %s && rm -rf %s'",
						sshCmd,
						remoteHost,
						remoteDestination,
						strings.Join(deleted, " "),
					),
				)
			},
		)
	}

	syncing = false
}

func watchDir(path string, fi os.FileInfo, err error) error {
	PanicIf(err)
	if fi.Mode().IsDir() { return watcher.Add(path) }
	return nil
}

func watchDirRecursive(path string, processor func(string)) {
	var err error
	watcher, err = fsnotify.NewWatcher()
	PanicIf(err)
	defer func() { PanicIf(watcher.Close()) }()

	PanicIf(filepath.Walk(path, watchDir))

	done := make(chan bool)

	go func() {
		for {
			select {
				case event := <-watcher.Events: processor(event.Name)
				case err := <-watcher.Errors: PanicIf(err)
			}
		}
	}()

	<-done
}

func stripTrailSlash(path string) string {
	last := len(path) - 1
	if (len(path) > 0) && (path[last:] == "/") { path = path[:last] }
	return path
}

func stopwatch(description string, operation func() bool) {
	fmt.Print(description + "... ")
	start := time.Now()
	if operation() { fmt.Println("done in " + time.Since(start).String()) }
}

func main() {
	args             := os.Args[1:]

	localDir         := stripTrailSlash(args[0])
	remoteHost       := args[1]
	remoteDir        := stripTrailSlash(args[2])
	identityFile     := args[3]
	connTimeout, err := strconv.Atoi(args[4])
	ignored          := args[4:]

	PanicIf(err)

	sshCmd := sshCommand(identityFile, connTimeout)

	go func() {
		for {
			fmt.Print("Establishing SSH Master connection... ")
			runCommand( // TODO: defer close?
				localDir,
				fmt.Sprintf(
					"%s -o ServerAliveInterval=%d -o ServerAliveCountMax=1 -M %s 'echo done && sleep infinity'",
					sshCmd,
					connTimeout,
					remoteHost,
				),
			)
			masterConnectionAlive = false
			time.Sleep(time.Duration(connTimeout) * time.Second)
		}
	}()

	watchDirRecursive(
		localDir,
		func(filename string) {
			filename = filename[len(localDir)+1:]

			isIgnored := false
			for _, regex := range ignored {
				if regexp.MustCompile(regex).MatchString(filename) {
					isIgnored = true
				}
			}
			if isIgnored { return }

			if len(files) == 0 {
				go func() {
					time.Sleep(5 * time.Second)
					syncFiles(localDir, remoteHost, remoteDir, sshCmd)
				}()
			}

			go func() {
				time.Sleep(500 * time.Millisecond)
				isLast := (len(files) > 0) && (files[len(files)-1] == filename)
				if isLast { syncFiles(localDir, remoteHost, remoteDir, sshCmd) }
			}()

			files = append(files, filename)
		},
	)
}
