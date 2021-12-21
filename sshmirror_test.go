package main

import (
	"encoding/json"
	"fmt"
	"github.com/0leksandr/my.go"
	"io/ioutil"
	"math/rand"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// TODO: test creating/removing/moving directories

var debug = false
var simpleFilenames = false
var delaysBasic = []float32{
	0,
	0.1,
}
var delaysMaster = []float32{
	0,
	0.1,
	0.5,
	1,
}

type TestFilename string
func (filename TestFilename) escaped() string {
	return "'" + strings.Join(strings.Split(string(filename), "'"), `'"'"'`) + "'"
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

type TestModificationsList []TestModificationInterface
func (modifications TestModificationsList) commandsVariants() [][]string {
	//commands := make([][]string, 0, len(modifications))
	//for _, modification := range modifications {
	//	commands = append(commands, modification.commandVariants())
	//}
	////return Twines(commands).([][]string)
	//return CartesianProducts(commands).([][]string)

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
	variantsAfter := chain.after.commandsVariants()
	scenarios := make([]TestScenario, 0, len(variantsBefore) * len(variantsAfter))
	for _, before := range variantsBefore {
		for _, after := range variantsAfter {
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
	if simpleFilenames {
		dir := "."
		if inTarget { dir += "/target" }
		fileIndex++
		return TestFilename(fmt.Sprintf("%s/file-%d", dir, fileIndex))
	}

	symbols := "abc,.;'[]<>?:\"{}123`~!@#$%^&*()-=_+ абв"
	symbols = "abcdefghijklmnop" // TODO: remove
	// TODO: guarantee uniqueness
	nrSymbols := rand.Intn(150) + 1
	dir := "./"
	if inTarget { dir += "target/" }
	var filename string
	for i := 0; i < nrSymbols; i++ {
		// TODO: check
		filename += string(symbols[rand.Intn(len(symbols))])
	}
	if my.InArray(
		filename,
		[]string{
			".",
			"..",
			".gitignore",
			"target",
		},
	) {
		return generateFilename(inTarget)
	}
	return TestFilename(dir + filename)
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
	return fmt.Sprintf("rm %s", filename.escaped())
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
					TestOptionalModification{write(a, 10)},
					TestSimpleModification{write(bExt, 11)},
					TestSimpleModification{write(cExt, 12)},
				},
				after: TestModificationsList{
					TestSimpleModification{move(bExt, a)},
					TestOptionalModification{write(a, 13)},
					TestSimpleModification{move(a, cExt)},
				},
			}
		})(generateFilename(true), generateFilename(false), generateFilename(false)),
		//(func(a TestFilename, b TestFilename, c TestFilename) TestModificationChain {
		//	return TestModificationChain{
		//		before: TestModificationsList{
		//			TestVariantsModification{[]string{
		//				create(a),
		//				write(a, 10),
		//			}},
		//			TestVariantsModification{[]string{
		//				create(b),
		//				write(b, 10),
		//			}},
		//		},
		//		after:  TestModificationsList{
		//			TestSimpleModification{move(b, c)},
		//			TestSimpleModification{move(a, b)},
		//		},
		//	}
		//})(generateFilename(true), generateFilename(true), generateFilename(true)),
	}
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
	for _, delaySeconds := range delaysBasic {
		for _, chain := range basicChains {
			chains = append(chains, TestModificationChain{
				before: chain.before,
				after:  mergeDelays(chain.after, delaySeconds),
			})
		}
	}
	for _, delaySeconds := range delaysMaster {
		chains = append(chains, TestModificationChain{
			before: masterChain.before,
			after:  mergeDelays(masterChain.after, delaySeconds),
		})
	}
	return chains
}

type TestConfig struct {
	IdentityFile   string
	RemoteAddress  string
	RemotePath     string
	TimeoutSeconds int
}

func TestIntegration(t *testing.T) {
	if false {
		debug = !debug
		simpleFilenames = !simpleFilenames
	}

	currentDir, err := os.Getwd()
	PanicIf(err)
	sandbox := fmt.Sprintf("%s/sandbox", currentDir)
	target := fmt.Sprintf("%s/target", sandbox)

	configFile, err := os.Open(fmt.Sprintf("%s/test-config.json", currentDir))
	PanicIf(err)
	defer func() { Must(configFile.Close()) }()
	testConfig := TestConfig{}
	Must(json.NewDecoder(configFile).Decode(&testConfig))

	sshmirror := exec.Command(
		"./sshmirror",
		"-i="+testConfig.IdentityFile,
		"-v=1",
		target,
		testConfig.RemoteAddress,
		testConfig.RemotePath,
	)
	sshmirror.Dir = currentDir
	defer func() { Must(sshmirror.Process.Kill()) }()
	go func() { Must(sshmirror.Run()) }()

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

	executeRemote := func(cmd string) []string {
		result := make([]string, 0)
		my.RunCommand(
			localDir,
			fmt.Sprintf(
				"%s %s -t \"cd %s && (%s)\"",
				sshCmd,
				testConfig.RemoteAddress,
				testConfig.RemotePath,
				cmd,
			),
			func(out string) {
				result = append(result, out)
			},
			nil,
		)
		return result
	}

	check := func() {
		hashCmd := `
(
 find . -type f -print0  | sort -z | xargs -0 sha1sum;
 find . \( -type f -o -type d \) -print0 | sort -z | xargs -0 stat -c '%n %a'
) | sha1sum
`
		var localHash string
		my.RunCommand(
			target,
			hashCmd,
			func(out string) {
				localHash = out
			},
			func(err string) { panic(err) },
		)

		remoteHash := executeRemote(hashCmd)
		if debug {
			my.Dump(localHash)
			my.Dump(remoteHash)
			for _, cmd := range []string{
				"find . -type f -print0  | sort -z | xargs -0 sha1sum;",
				"find . \\( -type f -o -type d \\) -print0 | sort -z | xargs -0 stat -c '%n %a'",
				hashCmd,
			} {
				my.Dump(cmd)
				local := make([]string, 0)
				my.RunCommand(
					target,
					cmd,
					func(out string) { local = append(local, out) },
					func(err string) { panic(err) },
				)
				my.Dump2(local)
				remote := executeRemote(cmd)
				my.Dump2(remote)
				my.Dump(reflect.DeepEqual(local, remote))
			}
		}
		if !reflect.DeepEqual([]string{localHash}, remoteHash) {
			t.Error("hashes mismatch", localHash, remoteHash)
			t.Fail()
		}
	}

	reset := func() {
		//resetCmd := "ls -1A . |grep -xv '.gitignore' |grep -xv 'target' |xargs -r -- rm"
		resetCmd := "find . -type f -not -name '.gitignore' -delete"
		my.RunCommand(
			sandbox,
			resetCmd,
			nil,
			func(err string) { panic(err) },
		)
		executeRemote(resetCmd)
	}
	reset()
	defer reset()

	chains := modificationChains()

	(func() {
		my.Dump2(time.Now())
		var nrScenarios int
		for _, chain := range chains { nrScenarios += len(chain.scenarios()) }
		my.Dump(nrScenarios)
	})()
	scenarioIdx := 0

	for _, chain := range chains {
		if debug { my.Dump2(chain) }
		for _, scenario := range chain.scenarios() {
			(func() {
				my.Dump(scenarioIdx)
				scenarioIdx++
			})()
			if debug { my.Dump2(scenario) }

			for _, command := range scenario.before {
				if debug { my.Dump(command) }

				if command != "" {
					my.RunCommand(
						sandbox,
						command,
						nil,
						func(err string) { panic(err) },
					)
				}
			}
			time.Sleep(time.Duration(testConfig.TimeoutSeconds) * time.Second)
			for _, command := range scenario.after {
				if debug { my.Dump(command) }

				my.RunCommand(
					sandbox,
					command,
					nil,
					func(err string) { panic(err) },
				)
			}
			time.Sleep(time.Duration(testConfig.TimeoutSeconds) * time.Second)
			check()
			reset()
		}
	}
}
