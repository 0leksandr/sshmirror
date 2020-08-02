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
import "./my"

var files []string
var watcher *fsnotify.Watcher

var syncing bool
var masterConnectionAlive bool
var syncingAwait bool

func runCommand(dir string, cmd string, onStdout func(string), onStderr func(string)) bool {
	command := exec.Command("sh", "-c", cmd)
	command.Dir = dir

	stdout, err := command.StdoutPipe()
	my.PanicIf(err)
	stdoutScanner := bufio.NewScanner(stdout)
	go func() {
		for stdoutScanner.Scan() {
			stdout := stdoutScanner.Text()
			fmt.Println(stdout)
			if onStdout != nil { onStdout(stdout) }
		}
	}()

	stderr, err := command.StderrPipe()
	my.PanicIf(err)
	stderrScanner := bufio.NewScanner(stderr)
	go func() {
		for stderrScanner.Scan() {
			stderr := stderrScanner.Text()
			_, err := fmt.Fprintln(os.Stderr, stderr)
			my.PanicIf(err)
			if onStderr != nil { onStderr(stderr) }
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
					nil,
					nil,
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
					nil,
					nil,
				)
			},
		)
	}

	syncing = false
}

func watchDirRecursive(path string, processor func(string)) {
	var err error
	watcher, err = fsnotify.NewWatcher()
	my.PanicIf(err)
	defer func() { my.PanicIf(watcher.Close()) }()

	my.PanicIf(filepath.Walk(
		path,
		func(path string, fi os.FileInfo, err error) error {
			my.PanicIf(err)
			if fi.Mode().IsDir() { return watcher.Add(path) }
			return nil
		},
	))

	done := make(chan bool)

	go func() {
		for {
			select {
				case event := <-watcher.Events: processor(event.Name)
				case err := <-watcher.Errors: my.PanicIf(err)
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
	args := os.Args[1:]

	localDir         := stripTrailSlash(args[0])
	remoteHost       := args[1]
	remoteDir        := stripTrailSlash(args[2])
	identityFile     := args[3]
	connTimeout, err := strconv.Atoi(args[4])
	ignored          := args[4:]

	my.PanicIf(err)

	sshCmd := fmt.Sprintf(
		"ssh -o ControlMaster=auto -o ControlPath=/tmp/ssh-%%r@%%h:%%p -o ConnectTimeout=%d -o ConnectionAttempts=1 -i %s",
		connTimeout,
		identityFile,
	)

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
				func(string) { masterConnectionAlive = true },
				func(string) { masterConnectionAlive = false },
			)
			masterConnectionAlive = false
			time.Sleep(time.Duration(connTimeout) * time.Second)
		}
	}()

	watchDirRecursive(
		localDir,
		func(filename string) {
			filename = filename[len(localDir)+1:]

			for _, regex := range ignored {
				if regexp.MustCompile(regex).MatchString(filename) {
					return
				}
			}

			doSync := func() { syncFiles(localDir, remoteHost, remoteDir, sshCmd) }

			if len(files) == 0 {
				go func() {
					time.Sleep(5 * time.Second)
					doSync()
				}()
			}

			go func() {
				time.Sleep(500 * time.Millisecond)
				isLast := (len(files) > 0) && (files[len(files)-1] == filename) // TODO: stop other threads instead
				if isLast { doSync() }
			}()

			files = append(files, filename)
		},
	)
}
