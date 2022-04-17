package main

import (
	"encoding/json"
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
// MAYBE: reproduce and investigate errors "rsync: link_stat * failed: No such file or directory (2)"
// MAYBE: test tricky filenames: `--`, `.`, `..`, `*`
// MAYBE: test ignored
// MAYBE: duplicate filenames in master chains

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

const FsnotifyCleanup = "sleep 0.002" // TODO: remove with FsnotifyListener

type TestFilename string
func (filename TestFilename) escaped() string {
	return wrapApostrophe(string(filename))
}

type TestModificationInterface interface {
	commandVariants() []string
}
type TestSimpleModification struct {
	command string
}
func (modification TestSimpleModification) commandVariants() []string {
	return []string{modification.command}
}
type TestOptionalModification struct {
	command string
}
func (modification TestOptionalModification) commandVariants() []string {
	return []string{
		"",
		modification.command,
	}
}
type TestVariantsModification struct {
	variants []string
}
func (modification TestVariantsModification) commandVariants() []string {
	return modification.variants
}

type TestScenario struct {
	before []string
	after  []string
}
func (scenario TestScenario) applyTarget(targetId int) {
	reg := regexp.MustCompile("\\[target]")
	target := fmt.Sprintf("target%d", targetId)
	for i, command := range scenario.before { scenario.before[i] = reg.ReplaceAllString(command, target) }
	for i, command := range scenario.after { scenario.after[i] = reg.ReplaceAllString(command, target) }
}

type TestModificationsList []TestModificationInterface
func (modifications TestModificationsList) commandsVariants() [][]string {
	//commands := make([][]string, 0, len(modifications))
	//for _, modification := range modifications {
	//	commands = append(commands, modification.commandVariants())
	//}
	////return Twines(commands).([][]string)
	//return CartesianProducts(commands).([][]string)

	if len(modifications) == 0 { return nil }
	nrVariants := 1
	for _, modification := range modifications {
		nrVariants *= len(modification.commandVariants())
	}
	variants0 := modifications[0].commandVariants()
	variants := make([][]string, 0, len(variants0))
	for _, variant := range variants0 { variants = append(variants, []string{variant}) }
	for _, modification := range modifications[1:] {
		modificationVariants := modification.commandVariants()
		newVariants := make([][]string, 0, len(variants) * len(modificationVariants))
		for _, variant := range variants {
			//variantCopy := make([]string, len(variant))
			//copy(variantCopy, variant)

			for _, command := range modificationVariants {
				newVariants = append(newVariants, append(variant, command))
			}
		}
		variants = newVariants
	}
	return variants
}

type TestModificationChain struct { // TODO: rename!
	before TestModificationsList
	after  TestModificationsList
}
func (chain TestModificationChain) scenarios() []TestScenario {
	variantsBefore := chain.before.commandsVariants()
	if len(variantsBefore) == 0 { variantsBefore = [][]string{{""}} }
	variantsAfter := chain.after.commandsVariants()
	scenarios := make([]TestScenario, 0, len(variantsBefore) * len(variantsAfter))
	for _, before := range variantsBefore {
		for _, after := range variantsAfter {
			//copyBefore := make([]string, len(before))
			//copy(copyBefore, before)
			//copyAfter := make([]string, len(after))
			//copy(copyAfter, after)

			scenarios = append(scenarios, TestScenario{
				before: before,
				after:  after,
			})
		}
	}
	return scenarios
}

var fileIndex = 0
func generateFilename(inTarget bool) TestFilename {
	dir := "."
	if inTarget { dir += "/target" }
	fileIndex++
	return TestFilename(fmt.Sprintf("%s/file-%d", dir, fileIndex))

	//var symbols []string
	//for _, symbol := range []rune("abc,.;'[]\\<>?\"{}|123`~!@#$%^&*()-=_+ абв🙂👍❗") {
	//	symbols = append(symbols, string(symbol))
	//}
	//symbols = append(
	//	symbols,
	//	"\\'",
	//	"\\\\'",
	//	"\\\\\\'",
	//	"\\\\\\\\'",
	//	"\\\\\\\\\\'",
	//)
	//nrSymbols := rand.Intn(150) + 1
	//dir := "./"
	//if inTarget { dir += "target/" }
	//var filename string
	//for i := 0; i < nrSymbols; i++ {
	//	filename += symbols[rand.Intn(len(symbols))]
	//}
	//if my.InArray(
	//	filename,
	//	[]string{
	//		".",
	//		"..",
	//		"*",
	//		".gitignore",
	//		"target",
	//	},
	//) {
	//	return generateFilename(inTarget)
	//}
	//return TestFilename(dir + filename)
}
func create(filename TestFilename) string {
	return fmt.Sprintf("touch %s", filename.escaped())
}
func write(filename TestFilename, size int) string {
	// TODO: guarantee uniqueness
	return fmt.Sprintf("cat /dev/urandom |head -c %d > %s", size, filename.escaped())
}
func move(from TestFilename, to TestFilename) string {
	return fmt.Sprintf("mv %s %s", from.escaped(), to.escaped())
}
func remove(filename TestFilename) string {
	return fmt.Sprintf("/bin/rm %s", filename.escaped())
}

func basicModificationChains() []TestModificationChain {
	return []TestModificationChain{
		(func(a TestFilename, b TestFilename) TestModificationChain {
			return TestModificationChain{
				before: TestModificationsList{
					TestSimpleModification{write(a, 10)},
					TestSimpleModification{write(b, 11)},
				},
				after: TestModificationsList{
					TestSimpleModification{remove(a)},
					TestSimpleModification{move(b, a)},
				},
			}
		})(generateFilename(true), generateFilename(true)),
		(func(a TestFilename, b TestFilename, cExt TestFilename) TestModificationChain {
			return TestModificationChain{
				before: TestModificationsList{
					TestVariantsModification{[]string{
						"",
						create(b),
						write(b, 10),
					}},
				},
				after: TestModificationsList{
					TestVariantsModification{[]string{
						create(a),
						write(a, 11),
					}},
					TestSimpleModification{move(a, b)},
					TestVariantsModification{[]string{
						remove(b),
						move(b, cExt),
					}},
					TestSimpleModification{FsnotifyCleanup},
				},
			}
		})(generateFilename(true), generateFilename(true), generateFilename(false)),
		(func(a TestFilename, b TestFilename, c TestFilename) TestModificationChain {
			return TestModificationChain{
				before: TestModificationsList{
					TestSimpleModification{create(a)},
				},
				after:  TestModificationsList{
					TestSimpleModification{move(a, b)},
					TestSimpleModification{move(b, c)},
				},
			}
		})(generateFilename(true), generateFilename(true), generateFilename(true)),
		(func(a TestFilename, b TestFilename, c TestFilename) TestModificationChain {
			return TestModificationChain{
				before: TestModificationsList{
					TestVariantsModification{[]string{
						create(a),
						write(a, 10),
					}},
					TestVariantsModification{[]string{
						"",
						create(b),
						write(b, 11),
					}},
					TestSimpleModification{write(c, 12)},
				},
				after: TestModificationsList{
					TestSimpleModification{move(a, b)},
					TestOptionalModification{write(b, 13)},
					TestVariantsModification{[]string{
						"",
						create(a),
						write(a, 14),
						move(c, a),
					}},
				},
			}
		})(generateFilename(true), generateFilename(true), generateFilename(true)),
		(func(a TestFilename, b TestFilename, c TestFilename) TestModificationChain {
			return TestModificationChain{
				before: TestModificationsList{
					TestSimpleModification{write(a, 10)},
					TestSimpleModification{write(b, 11)},
					TestOptionalModification{write(c, 12)},
				},
				after: TestModificationsList{
					TestSimpleModification{move(a, c)},
					TestSimpleModification{move(b, a)},
					TestSimpleModification{move(c, b)},
				},
			}
		})(generateFilename(true), generateFilename(true), generateFilename(true)),
		(func(a TestFilename, b TestFilename, cExt TestFilename) TestModificationChain {
			return TestModificationChain{
				before: TestModificationsList{
					TestSimpleModification{write(a, 10)},
				},
				after: TestModificationsList{
					TestSimpleModification{move(a, cExt)},
					TestVariantsModification{[]string{
						create(b),
						write(b, 11),
					}},
				},
			}
		})(generateFilename(true), generateFilename(true), generateFilename(false)),
		(func(a TestFilename, bExt TestFilename, cExt TestFilename) TestModificationChain {
			return TestModificationChain{
				before: TestModificationsList{
					//TestOptionalModification{write(a, 10)}, // MAYBE: find a normal way to test, and uncomment
					TestSimpleModification{write(a, 10)},
					TestSimpleModification{write(bExt, 11)},
					TestSimpleModification{write(cExt, 12)},
				},
				after: TestModificationsList{
					TestSimpleModification{move(bExt, a)},
					TestOptionalModification{write(a, 13)},
					TestSimpleModification{move(a, cExt)},
					TestSimpleModification{FsnotifyCleanup},
				},
			}
		})(generateFilename(true), generateFilename(false), generateFilename(false)),
		(func(a TestFilename, b TestFilename, c TestFilename) TestModificationChain {
			return TestModificationChain{
				before: TestModificationsList{
					TestVariantsModification{[]string{
						create(a),
						write(a, 10),
					}},
					TestVariantsModification{[]string{
						create(b),
						write(b, 10),
					}},
				},
				after:  TestModificationsList{
					TestSimpleModification{move(b, c)},
					TestSimpleModification{move(a, b)},
				},
			}
		})(generateFilename(true), generateFilename(true), generateFilename(true)),
		(func(a TestFilename, b TestFilename, c TestFilename) TestModificationChain {
			return TestModificationChain{
				before: TestModificationsList{
					TestSimpleModification{write(a, 10)},
					TestSimpleModification{write(b, 11)},
				},
				after:  TestModificationsList{
					TestSimpleModification{move(b, c)},
					TestSimpleModification{move(a, b)},
					TestVariantsModification{[]string{
						write(c, 12),
						write(b, 13),
					}},
				},
			}
		})(generateFilename(true), generateFilename(true), generateFilename(true)),
		(func(a TestFilename, b TestFilename, c TestFilename) TestModificationChain {
			return TestModificationChain{
				before: TestModificationsList{
					TestVariantsModification{[]string{
						create(a),
						write(a, 10),
					}},
					TestVariantsModification{[]string{
						create(b),
						write(b, 11),
					}},
					TestVariantsModification{[]string{
						create(c),
						write(c, 12),
					}},
				},
				after:  TestModificationsList{
					TestSimpleModification{move(a, b)},
					TestSimpleModification{move(b, c)},
					TestSimpleModification{remove(c)},
				},
			}
		})(generateFilename(true), generateFilename(true), generateFilename(true)),
	}
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
	basicChains := basicModificationChains()
	chains := make([]TestModificationChain, 0, (len(basicChains) * len(delaysBasic)) + len(delaysMaster))
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
	for _, chain := range basicChains {
		masterChain.before = append(masterChain.before, simplify(chain.before)...)
		masterChain.after = append(masterChain.after, simplify(chain.after)...)
	}
	for _, delaySeconds := range delaysMaster {
		chains = append(chains, TestModificationChain{
			before: masterChain.before,
			after:  mergeDelays(masterChain.after, delaySeconds),
		})
	}
	for _, delaySeconds := range delaysBasic {
		for _, chain := range basicChains {
			chains = append(chains, TestModificationChain{
				before: chain.before,
				after:  mergeDelays(chain.after, delaySeconds),
			})
		}
	}
	chains = append(chains, filenameModificationChains()...)
	return chains
}

type TestConfig struct {
	IdentityFile    string
	RemoteAddress   string
	RemotePath      string
	TimeoutSeconds  int
	NrThreads       int
	Debug           bool
	IntegrationTest bool
}
func (config TestConfig) IsSet() bool {
	value := reflect.ValueOf(config)
	for i := 0; i < value.NumField(); i++ {
		field := value.Field(i)
		if field.Kind() != reflect.Bool {
			if reflect.New(field.Type()).Elem().Interface() == field.Interface() { return false }
		}
	}
	return true
}

func TestIntegration(t *testing.T) {
	currentDir, err := os.Getwd()
	PanicIf(err)
	sandbox := fmt.Sprintf("%s/sandbox", currentDir)

	configFile, err := os.Open(fmt.Sprintf("%s/test-config.json", currentDir))
	PanicIf(err)
	defer func() { Must(configFile.Close()) }()
	testConfig := TestConfig{}
	Must(json.NewDecoder(configFile).Decode(&testConfig))
	if !testConfig.IsSet() { panic("config is not set") }

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
		loggers = append(loggers, &InMemoryLogger{})
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
					"-v=1",
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
						if testConfig.Debug {
							my.Dump("logs:")
							logger.Print()
							panic("test failed")
						}
					}
				}

				scenario.applyTarget(processId)

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

					if command != "" && command != FsnotifyCleanup {
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
			if testConfig.Debug {
				loggers[processId-1] = nil
				go func() {
					time.Sleep(10 * time.Second)
					for _, foreignLogger := range loggers {
						if foreignLogger != nil {
							my.Dump("open logs:")
							foreignLogger.Print()
							panic("test")
						}
					}
				}()
			}

			wg.Done()
		}(i + 1)
	}
	wg.Wait()
}
