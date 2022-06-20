package main

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

type RemoteClient interface {
	io.Closer
	LoggerAware
	Update([]Updated) CancellableContext
	Delete([]Deleted) error
	Move([]Moved) error
	Ready() *Locker
}

type sshClient struct { // TODO: rename
	RemoteClient
	io.Closer
	config      Config
	sshCmd      string
	controlPath string
	masterReady *Locker
	done        bool // MAYBE: masterConnectionProcess
	logger      Logger
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

	var waitingMaster Locker

	client := &sshClient{
		config:      config,
		sshCmd:      sshCmd,
		controlPath: controlPath,
		masterReady: &waitingMaster,
		logger:      Logger{
			debug: NullLogger{},
			error: StdErrLogger{},
		},
	}

	client.masterReady.Lock()
	go client.keepMasterConnection()

	return client
}
func (client *sshClient) Close() error {
	client.done = true
	client.closeMaster()
	_ = os.Remove(client.controlPath)
	return nil
}
func (client *sshClient) Update(updated []Updated) CancellableContext {
	escapedFilenames := make([]string, 0, len(updated))
	for _, modification := range updated {
		escapedFilenames = append(escapedFilenames, modification.path.original.Escaped())
	}

	command := client.startCommand(
		fmt.Sprintf(
			"rsync --checksum --recursive --links --perms --times --group --owner --executability --compress --relative --rsh='%s' -- %s %s:%s",
			client.sshCmd,
			strings.Join(escapedFilenames, " "),
			client.config.remoteHost,
			client.config.remoteDir,
		),
		true,
		nil,
	)
	return CancellableContext{
		Result: func() error { return command.Wait() }, // TODO: ensure an error is returned on cancel
		Cancel: func() { Must(command.Process.Signal(syscall.SIGTERM)) },
	}
}
func (client *sshClient) Delete(deleted []Deleted) error {
	escapedFilenames := make([]string, 0, len(deleted))
	for _, modification := range deleted {
		escapedFilenames = append(escapedFilenames, modification.path.original.Escaped())
	}

	if client.runRemoteCommand(fmt.Sprintf(
		"rm -rf -- %s", // MAYBE: something more reliable
		strings.Join(escapedFilenames, " "),
	)) {
		return nil
	} else {
		return errors.New("cound not delete") // MAYBE: actual error
	}
}
func (client *sshClient) Move(moved []Moved) error {
	commands := make([]string, 0, len(moved))
	for _, modification := range moved {
		commands = append(commands, fmt.Sprintf(
			"mv -- %s %s",
			modification.from.original.Escaped(),
			modification.to.original.Escaped(),
		))
	}

	if client.runRemoteCommand(strings.Join(commands, " && ")) {
		return nil
	} else {
		return errors.New("could not move") // MAYBE: actual error
	}
}
func (client *sshClient) Ready() *Locker {
	return client.masterReady
}
func (client *sshClient) SetLogger(logger Logger) {
	client.logger = logger
}
func (client *sshClient) keepMasterConnection() {
	client.closeMaster()

	for {
		fmt.Print("Establishing SSH Master connection... ") // MAYBE: stopwatch

		// MAYBE: check if it doesn't hang on server after disconnection
		client.runCommand(
			fmt.Sprintf(
				"%s -o ServerAliveInterval=%d -o ServerAliveCountMax=1 -M %s 'echo done && sleep infinity'",
				client.sshCmd,
				client.config.connTimeout,
				client.config.remoteHost,
			),
			false,
			func(out string) {
				fmt.Println(out)
				client.logger.Debug("master ready")
				client.masterReady.Unlock() // MAYBE: ensure this happens only once
			},
		)

		client.masterReady.Lock()
		client.closeMaster()
		if client.done { break }
		time.Sleep(time.Duration(client.config.connTimeout) * time.Second)
	}
}
func (client *sshClient) closeMaster() {
	client.runCommand(
		fmt.Sprintf("%s -O exit %s 2>/dev/null", client.sshCmd, client.config.remoteHost),
		false,
		nil,
	)
}
func (client *sshClient) runCommand(command string, localDir bool, onStdout func(string)) bool {
	return client.startCommand(command, localDir, onStdout).Wait() == nil
}
func (client *sshClient) startCommand(command string, localDir bool, onStdout func(string)) *exec.Cmd {
	client.logger.Debug("running command", command)
	var dir string
	if localDir { dir = client.config.localDir }
	return StartCommand(
		dir,
		command,
		onStdout,
		func(err string) { client.logger.Error(fmt.Sprintf("command: %s; error: %s", command, err)) },
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
		false,
		nil,
	)
}
