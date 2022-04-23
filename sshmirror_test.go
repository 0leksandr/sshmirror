package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/0leksandr/my.go"
	"io/ioutil"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// TODO: test modifying root dir: https://github.com/0leksandr/sshmirror/issues/4
// TODO: test tabs, zero-symbols and others in filenames
// MAYBE: reproduce and investigate errors "rsync: link_stat * failed: No such file or directory (2)"
// MAYBE: test tricky filenames: `--`, `.`, `..`, `*`, `:`
// MAYBE: test ignored
// MAYBE: duplicate filenames in master chains
// MAYBE: test fallback

var delaysBasic = [...]float32{ // TODO: non-constant delays (pseudo-random pauses)
	0.,
	0.1,
	0.6,
}
var delaysMaster = [...]float32{
	0.,
	0.1,
	0.4,
	0.6,
	1.,
}

func filenameModificationChains() []TestModificationChain {
	apostrophes := []string{
		"\\'",
		"\\\\'",
		"\\\\\\'",
		"\\\\\\\\'",
		"\\\\\\\\\\'",
	}
	filenames := make([]TestFilename, 0, len(apostrophes))
	for i := 0; i <= len(apostrophes); i++ {
		filenames = append(
			filenames,
			TestFilename("abc,.;'[]\\<>?\"{}|123`~!@#$%^&*()-=_+ абв🙂👍❗" + strings.Join(apostrophes[:i], "")),
		)
	}
	for i, filename := range filenames { filenames[i] = "./target/" + filename }
	chains := []func(filename TestFilename) TestModificationChain{
		func(filename TestFilename) TestModificationChain {
			return TestModificationChain{after: TestModificationsList{TestSimpleModification{create(filename)}}}
		},
		func(filename TestFilename) TestModificationChain {
			return TestModificationChain{after: TestModificationsList{TestSimpleModification{write(filename, 10)}}}
		},
		func(filename TestFilename) TestModificationChain {
			filename2 := filename + "$"
			return TestModificationChain{
				before: TestModificationsList{TestSimpleModification{create(filename2)}},
				after: TestModificationsList{TestSimpleModification{move(filename2, filename)}},
			}
		},
		func(filename TestFilename) TestModificationChain {
			return TestModificationChain{
				before: TestModificationsList{TestSimpleModification{create(filename)}},
				after: TestModificationsList{TestSimpleModification{remove(filename)}},
			}
		},
	}
	chains2 := make([]TestModificationChain, 0, len(filenames) * len(chains))
	for _, filename := range filenames {
		for _, chain := range chains {
			chains2 = append(chains2, chain(filename))
		}
	}
	return chains2
}
func modificationChains() []TestModificationChain {
	basicCases := basicModificationCases()
	chains := make([]TestModificationChain, 0, (len(basicCases) * len(delaysBasic)) + len(delaysMaster))
	simplify := func(modifications []TestModificationInterface) []TestModificationInterface {
		simplified := make([]TestModificationInterface, 0, len(modifications))
		for _, modification := range modifications {
			variants := modification.commandVariants()
			simplified = append(simplified, TestSimpleModification{variants[len(variants) - 1]})
		}
		return simplified
	}
	mergeDelays := func(modifications []TestModificationInterface, delaySeconds float32) []TestModificationInterface {
		if len(modifications) == 0 { return nil }
		merged := make([]TestModificationInterface, 0, len(modifications) * 2 + 1)
		merged = append(merged, modifications[0])
		for i := 1; i < len(modifications); i++ {
			merged = append(merged, TestSimpleModification{fmt.Sprintf("sleep %f", delaySeconds)})
			merged = append(merged, modifications[i])
		}
		return merged
	}
	var masterChain TestModificationChain
	for _, testCase := range basicCases {
		masterChain.before = append(masterChain.before, simplify(testCase.chain.before)...)
		masterChain.after = append(masterChain.after, simplify(testCase.chain.after)...)
	}
	for _, delaySeconds := range delaysMaster {
		chains = append(chains, TestModificationChain{
			before: masterChain.before,
			after:  mergeDelays(masterChain.after, delaySeconds),
		})
	}
	for _, delaySeconds := range delaysBasic {
		for _, testCase := range basicCases {
			chains = append(chains, TestModificationChain{
				before: testCase.chain.before,
				after:  mergeDelays(testCase.chain.after, delaySeconds),
			})
		}
	}
	chains = append(chains, filenameModificationChains()...)
	return chains
}

type TestConfig struct {
	IdentityFile     string
	RemoteAddress    string
	RemotePath       string
	TimeoutSeconds   int
	NrThreads        int
	ErrorCmd         string
	LastDelaySeconds int
	StopOnFail       bool
	IntegrationTest  bool
}
func (TestConfig) New(filename string) TestConfig {
	configFile, err := os.Open(filename)
	PanicIf(err)
	defer func() { Must(configFile.Close()) }()
	testConfig := TestConfig{}
	Must(json.NewDecoder(configFile).Decode(&testConfig))

	fileContent, err := ioutil.ReadFile(filename)
	PanicIf(err)
	Must(testConfig.check(fileContent))

	return testConfig
}
func (config TestConfig) check(originalContent []byte) error {
	value := reflect.ValueOf(config)
	for i := 0; i < value.NumField(); i++ {
		field := value.Field(i)
		fieldName := value.Type().Field(i).Name
		allowEmpty := field.Kind() == reflect.Bool || fieldName == "ErrorCmd" || fieldName == "LastDelaySeconds"
		if !allowEmpty && reflect.New(field.Type()).Elem().Interface() == field.Interface() {
			return errors.New(fmt.Sprintf("field %s is empty", fieldName))
		}
	}

	buffer := &bytes.Buffer{}
	if err := json.NewEncoder(buffer).Encode(config); err != nil { return err }
	replaceBlanks := func(text []byte) []byte { // MAYBE: something smarter
		for from, to := range map[string][]byte{
			"\\n *": {},
			"\": +": []byte("\":"),
		} {
			text = regexp.MustCompile(from).ReplaceAll(text, to)
		}
		return text
	}
	if !bytes.Equal(replaceBlanks(originalContent), replaceBlanks(buffer.Bytes())) {
		return errors.New("some fields are missing")
	}

	return nil
}

func TestIntegration(t *testing.T) {
	currentDir, err := os.Getwd()
	PanicIf(err)
	sandbox := fmt.Sprintf("%s/sandbox", currentDir)
	testConfig := TestConfig{}.New(fmt.Sprintf("%s/test-config.json", currentDir))

	controlPathFile, err := ioutil.TempFile("", "sshmirror-test-")
	PanicIf(err)
	controlPath := controlPathFile.Name()
	Must(os.Remove(controlPath))
	defer func() { Must(os.Remove(controlPath)) }()
	sshCmd := fmt.Sprintf("ssh -t -o ControlPath=%s -i %s", controlPath, testConfig.IdentityFile)
	var masterConnectionReady sync.WaitGroup
	masterConnectionReady.Add(1)
	go func() {
		my.RunCommand(
			currentDir,
			fmt.Sprintf("%s -M %s -t 'echo done && sleep 1000'", sshCmd, testConfig.RemoteAddress),
			func(string) {
				masterConnectionReady.Done()
			},
			nil,
		)
		panic("master connection dead")
	}()
	masterConnectionReady.Wait()

	executeRemote := func(remotePath string, cmd string) []string {
		result := make([]string, 0)
		my.RunCommand(
			"",
			fmt.Sprintf(
				"%s %s -t \"cd %s && (%s)\"",
				sshCmd,
				testConfig.RemoteAddress,
				remotePath,
				cmd,
			),
			func(out string) {
				result = append(result, out)
			},
			nil,
		)
		return result
	}

	var SUTsDone sync.WaitGroup

	reset := func(remotePath string, localPath string, full bool) {
		var resetCmd string
		if full {
			//resetCmd = "find . -not -path . -not -name '.gitignore' -exec rm -r {} +"
			resetCmd = "find . -type f -not -name '.gitignore' -delete && find . -type d -delete"
		} else {
			//resetCmd = "find . -not -name '.gitignore' -not -name 'target' -delete"
			resetCmd = "find . -type f -not -name '.gitignore' -delete"
		}
		my.RunCommand(
			localPath,
			resetCmd,
			nil,
			func(err string) { panic(err) },
		)
		executeRemote(remotePath, resetCmd)
	}
	reset(testConfig.RemotePath, sandbox, true)
	var cleanUp sync.WaitGroup
	cleanUp.Add(1)
	defer func() {
		SUTsDone.Wait()
		reset(testConfig.RemotePath, sandbox, true)
		cleanUp.Done()
	}()

	chains := modificationChains()

	my.Dump2(time.Now())
	var nrScenarios int
	for _, chain := range chains { nrScenarios += len(chain.scenarios()) }
	my.Dump(nrScenarios)

	scenarios := make(chan TestScenario, nrScenarios)
	for _, chain := range chains {
		for _, scenario := range chain.scenarios() {
			scenarios <- scenario
		}
	}
	close(scenarios)

	loggers := make([]*InMemoryLogger, 0, testConfig.NrThreads)
	for i := 0; i < testConfig.NrThreads; i++ {
		loggers = append(loggers, &InMemoryLogger{
			timestamps: true,
			errorLogger: (func() ErrorLogger {
				if testConfig.ErrorCmd != "" {
					return ErrorCmdLogger{
						errorCmd: testConfig.ErrorCmd,
					}
				} else {
					return StdErrLogger{}
				}
			})(),
		})
	}

	var wg sync.WaitGroup
	wg.Add(testConfig.NrThreads)
	scenarioIdx := 0
	for i := 0; i < testConfig.NrThreads; i++ {
		go func(processId int) {
			processDir := fmt.Sprintf("process%d", processId)
			targetDir := fmt.Sprintf("%s/target", processDir)
			localSandbox := fmt.Sprintf("%s/%s", sandbox, processDir)
			remoteSandbox := fmt.Sprintf("%s/%s", testConfig.RemotePath, processDir)
			localTarget := fmt.Sprintf("%s/%s", sandbox, targetDir)
			remoteTarget := fmt.Sprintf("%s/%s", testConfig.RemotePath, targetDir)
			mkdir := fmt.Sprintf("mkdir -p %s", targetDir)
			my.RunCommand(sandbox, mkdir, nil, func(err string) { panic(err) })
			executeRemote(testConfig.RemotePath, mkdir)

			logger := loggers[processId - 1]

			var syncing Locker
			SUTsDone.Add(1)
			if testConfig.IntegrationTest {
				command := exec.Command(
					"./sshmirror",
					"-i="+testConfig.IdentityFile,
					"-v=0",
					localTarget,
					testConfig.RemoteAddress,
					remoteTarget,
				)
				command.Dir = currentDir
				defer func() {
					Must(command.Process.Kill())
					SUTsDone.Done()
				}()
				Must(command.Start())
			} else {
				client := SSHMirror{}.New(Config{
					localDir:     localTarget,
					remoteHost:   testConfig.RemoteAddress,
					remoteDir:    remoteTarget,
					identityFile: testConfig.IdentityFile,
					connTimeout:  testConfig.TimeoutSeconds,
					errorCmd:     testConfig.ErrorCmd,
				})
				client.SetLogger(logger)
				client.onReady = func() {
					client.logger.Debug("client.onReady")
					syncing.Unlock()
				}
				go client.Run()
				defer func() {
					Must(client.Close())
					SUTsDone.Done()
				}()
			}

			awaitSync := func() {
				if testConfig.IntegrationTest {
					time.Sleep(time.Duration(testConfig.TimeoutSeconds) * time.Second)
				} else {
					syncing.Wait()
				}
			}

			for scenario := range scenarios {
				(func() {
					my.Dump(scenarioIdx)
					scenarioIdx++ // MAYBE: atomic
				})()
				logger.Debug("scenario", scenario)

				check := func() {
					localPath := localTarget
					remotePath := remoteTarget
					hashCmd := `
(
  find . -type f -print0  | LC_ALL=C sort -z | xargs -0 sha1sum;
  find . \( -type f -o -type d \) -print0 | LC_ALL=C sort -z | xargs -0 stat -c '%n %a'
) | sha1sum
`
					var localHash string
					for localHash == "" {
						my.RunCommand(
							localPath,
							hashCmd,
							func(out string) {
								localHash = out
							},
							func(err string) { panic(err) },
						)
					}

					var remoteHash []string
					for len(remoteHash) == 0 {
						remoteHash = executeRemote(remotePath, hashCmd)
					}

					if !reflect.DeepEqual([]string{localHash}, remoteHash) {
						t.Error("hashes mismatch", localHash, remoteHash)

						logger.Debug("check failed")
						logger.Debug("processId", processId)
						for _, cmd := range []string{
							"find . -type f -print0 | LC_ALL=C sort -z | xargs -0 -r sha1sum",
							"find . \\( -type f -o -type d \\) -print0 | LC_ALL=C sort -z | xargs -0 stat -c '%n %a'",
							hashCmd,
							"tree ../..",
							"cat -- *",
						} {
							local := make([]string, 0)
							my.RunCommand(
								localPath,
								cmd,
								func(out string) { local = append(local, out) },
								func(err string) { panic(err) },
							)
							remote := executeRemote(remotePath, cmd)
							logger.Debug("cmd", cmd)
							logger.Debug("local", local)
							logger.Debug("remote", remote)
							logger.Debug("equal", reflect.DeepEqual(local, remote))
						}
						if testConfig.StopOnFail {
							my.Dump("logs:")
							logger.Print()
							panic("test failed")
						}
					}
				}

				//scenario.applyTarget(processId)

				for _, command := range scenario.before {
					logger.Debug("command.before", command)

					if command != "" {
						if !testConfig.IntegrationTest { syncing.Lock() }

						my.RunCommand(
							localSandbox,
							command,
							nil,
							func(err string) { panic(err) },
						)
					}
				}
				awaitSync()

				for _, command := range scenario.after {
					logger.Debug("command.after", command)

					if command != "" && command != MovementCleanup {
						if !testConfig.IntegrationTest { syncing.Lock() }

						my.RunCommand(
							localSandbox,
							command,
							nil,
							func(err string) { panic(err) },
						)
					}
				}
				awaitSync()
				check()
				reset(remoteSandbox, localSandbox, false)
			}
			if testConfig.LastDelaySeconds != 0 {
				loggers[processId-1] = nil
				go func() {
					time.Sleep(time.Duration(testConfig.LastDelaySeconds) * time.Second)
					for _, foreignLogger := range loggers {
						if foreignLogger != nil {
							my.Dump("open logs:")
							foreignLogger.Print()
							panic("test failed")
						}
					}
				}()
			}

			wg.Done()
		}(i + 1)
	}
	wg.Wait()
}
