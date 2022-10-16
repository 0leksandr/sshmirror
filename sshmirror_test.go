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
// TODO: test files and directories with same name
// TODO: test moving a/b/c > a/d > a/b/c/e, a/b/c > a/b > a/b
// MAYBE: reproduce and investigate errors "rsync: link_stat * failed: No such file or directory (2)"
// MAYBE: test tricky filenames: `--`, `.`, `..`, `*`, `:`, `\r`
// MAYBE: test ignored
// MAYBE: duplicate filenames in master chains
// MAYBE: test fallback
// MAYBE: test fsnotify watcher
// MAYBE: emulate slow/bad connection for tricky/complex tests
// MAYBE: random test fails:
//        + initializing master connection count as syncing time, and thus timed out
//        - order of updates changed. WTF?
//        - syncing times out, because (almost) 2 syncing operations were count in
//        - test did not wait for syncing to end. Wait for master was done (much) faster for test, than for client.
//          Also, modification was delayed. As a result, `client.onReady` (for master ready) was triggered between
//          actual modification and received one. Test treated it as ready after modification was processed
//          Chronology:
//          - master ready
//          - test received it, made actual modification
//          - client received it, checking for "zero"-modifications
//          - no modifications found, triggering `onReady` (as of master)
//          - modification received, but too late
//        - `mv a ../b && mv ../c a` is treated as `mv a a` in inotify
//        ± nil pointer dereference in `command.ProcessState.Exited()` during cancelling upload
//        - inotify missed files in new subdirectories (see main file)
//        - a file moving outside and inside (so that nothing changed, file moving to itself tracked) didn't
//          call `onReady`, test timed out
//        - created directories are treated as created files. And inotify logs are missing. WTF?
// MAYBE: count every integration test case as a separate test (in IDE's progress bar)
// WONTDO: autogenerated TestModificationsList with combinations of modifications for integration test. Nr combinations
//         (if nr files = 3, nr external files = 2, nr modifications = 4) = (3 + 3 + (5 * 4 - 1)) ^ 4 = 390625

var delaysBasic = [...]float32{ // TODO: non-constant delays (pseudo-random pauses)
	0.,
	0.1,
	0.6,
	1.,
}
var delaysMaster = [...]float32{
	0.,
	0.1,
	0.4,
	0.6,
	1.,
}

const MovementCleanup = "sleep 0.003" // MAYBE: come up with something better
const TargetDir = "target"

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
	files []Filename
	dirs  []Filename
	after []string // MAYBE: type CommandString
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

type TestModificationChain struct { // MAYBE: rename
	files []Filename // MAYBE: split required and optional
	dirs  []Filename
	after TestModificationsList
}
func (chain TestModificationChain) scenarios() []TestScenario {
	variantsAfter := chain.after.commandsVariants()
	scenarios := make([]TestScenario, 0, len(variantsAfter))
	for _, after := range variantsAfter {
		//copyBefore := make([]string, len(files))
		//copy(copyBefore, files)
		//copyAfter := make([]string, len(after))
		//copy(copyAfter, after)

		scenarios = append(scenarios, TestScenario{
			files: chain.files,
			dirs:  chain.dirs,
			after: after,
		})
	}
	return scenarios
}

//type LocalDelayClient struct {
//	RemoteClient
//	delay     time.Duration
//	dirFrom   Filename
//	dirTo     Filename
//	commander RemoteCommander
//	locker    *Locker
//}
//func (LocalDelayClient) New(
//	delay time.Duration,
//	dirFrom Filename,
//	dirTo Filename,
//	commander RemoteCommander,
//) LocalDelayClient {
//	return LocalDelayClient{
//		delay:     delay,
//		dirFrom:   dirFrom,
//		dirTo:     dirTo,
//		commander: commander,
//		locker:    &Locker{},
//	}
//}
//func (client LocalDelayClient) Close() error {
//	return nil
//}
//func (client LocalDelayClient) Update(updated []Updated) CancellableContext {
//	cmds := make([]string, 0, len(updated))
//	for _, _updated := range updated {
//		path := string(os.PathSeparator) + _updated.path.original.Escaped()
//		cmds = append(cmds, fmt.Sprintf(
//			"cp -r %s %s",
//			client.dirFrom.Escaped() + path,
//			client.dirTo.Escaped() + path,
//		))
//	}
//	client.run(cmds)
//	return CancellableContext{
//		Result: func() error { return nil },
//		Cancel: func() { panic("cannot cancel") },
//	}
//}
//func (client LocalDelayClient) InPlace(inPlace []InPlaceModification) error {
//	cmds := make([]string, 0, len(inPlace))
//	for _, modification := range inPlace {
//		cmds = append(cmds, modification.Command(client.commander))
//	}
//	client.run(cmds)
//	return nil
//}
//func (client LocalDelayClient) Ready() *Locker {
//	return client.locker
//}
//func (client LocalDelayClient) run(cmds []string) {
//	time.Sleep(client.delay)
//	my.RunCommand(
//		"",
//		strings.Join(cmds, " && "),
//		nil,
//		func(err string) { panic(err) },
//	)
//}

var fileIndex = 0
func generateFilenamePart() Filename {
	fileIndex++
	return Filename(fmt.Sprintf("file-%d", fileIndex))

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
	//		TargetDir,
	//	},
	//) {
	//	return generateFilename(inTarget)
	//}
	//return Filename(dir + filename)
}
func generateFilename(directories ...Filename) Filename {
	directoriesStr := make([]string, 0, len(directories) + 1)
	directoriesStr = append(directoriesStr, TargetDir)
	for _, directory := range directories { directoriesStr = append(directoriesStr, string(directory)) }
	sep := string(os.PathSeparator)
	return Filename(strings.Join(directoriesStr, sep) + sep + string(generateFilenamePart()))
}
func generateFilenamesChain(length int) []Filename {
	chain := make([]Filename, 0, length)
	chain = append(chain, generateFilename())
	for i := 1; i < length; i++ {
		chain = append(chain, chain[len(chain)-1] + Filename(os.PathSeparator) + generateFilenamePart())
	}
	return chain
}

func create(filename Filename) string { // MAYBE: touch
	return fmt.Sprintf("touch %s", filename.Escaped())
}
var contentIndex = 0
func write(filename Filename) string {
	var _mkdir string
	dir := Path{}.New(filename, false).Parent()
	if len(dir.parts) > 0 {
		_mkdir = mkdir(dir.original) + " && " +
			"sleep 0.05 && " // PRIORITY: remove after new subdirectories are being scanned
	}
	contentIndex++
	return _mkdir + fmt.Sprintf("echo %d > %s", contentIndex, filename.Escaped())
}
func move(from, to Filename) string {
	return fmt.Sprintf("mv %s %s", from.Escaped(), to.Escaped())
}
func remove(filename Filename) string {
	return fmt.Sprintf("/bin/rm %s", filename.Escaped())
}
func mkdir(dir Filename) string {
	return UnixCommander{}.MkdirCommand(Path{}.New(dir, true))
}

func basicModificationChains() []TestModificationChain {
	return []TestModificationChain{
		(func(a Filename) TestModificationChain {
			return TestModificationChain{
				files: []Filename{},
				after: TestModificationsList{
					TestSimpleModification{create(a)},
				},
			}
		})(generateFilename()),
		(func(a, b Filename) TestModificationChain {
			return TestModificationChain{
				files: []Filename{a, b},
				after: TestModificationsList{
					TestSimpleModification{remove(a)},
					TestSimpleModification{move(b, a)},
				},
				//Moved{b, a},
			}
		})(generateFilename(), generateFilename()),
		(func(a, b, cExt Filename) TestModificationChain {
			return TestModificationChain{
				files: []Filename{b},
				after: TestModificationsList{
					TestSimpleModification{write(a)},
					TestSimpleModification{move(a, b)},
					TestVariantsModification{[]string{
						remove(b),
						move(b, cExt),
					}},
					TestSimpleModification{MovementCleanup},
				},
				// Deleted{a},
				// Deleted{b},
			}
		})(generateFilename(), generateFilename(), generateFilename("..")),
		(func(a, b, c Filename) TestModificationChain {
			return TestModificationChain{
				files: []Filename{a},
				after: TestModificationsList{
					TestSimpleModification{move(a, b)},
					TestSimpleModification{move(b, c)},
				},
				//Moved{a, c},
				//Deleted{b},
			}
		})(generateFilename(), generateFilename(), generateFilename()),
		(func(a, b Filename) TestModificationChain {
			return TestModificationChain{
				files: []Filename{a, b},
				after: TestModificationsList{
					TestSimpleModification{move(a, b)},
					TestSimpleModification{write(a)},
				},
			}
		})(generateFilename(), generateFilename()),
		(func(a, b, c Filename) TestModificationChain {
			return TestModificationChain{
				files: []Filename{a, b, c},
				after: TestModificationsList{
					TestSimpleModification{move(a, b)},
					TestSimpleModification{write(b)},
					TestVariantsModification{[]string{
						"",
						create(a),
						write(a),
						move(c, a),
					}},
					TestSimpleModification{write(a)},
				},
			}
		})(generateFilename(), generateFilename(), generateFilename()),
		(func(a, b, c Filename) TestModificationChain {
			return TestModificationChain{
				files: []Filename{a, b, c},
				after: TestModificationsList{
					TestSimpleModification{move(a, c)},
					TestSimpleModification{move(b, a)},
					TestSimpleModification{move(c, b)},
				},
			}
		})(generateFilename(), generateFilename(), generateFilename()),
		(func(a, b, cExt Filename) TestModificationChain { // group begin: tricky Watcher cases
			return TestModificationChain{
				files: []Filename{a},
				after: TestModificationsList{
					TestSimpleModification{move(a, cExt)},
					TestVariantsModification{[]string{
						create(b),
						write(b),
					}},
				},
			}
		})(generateFilename(), generateFilename(), generateFilename("..")),
		(func(a, bExt, cExt Filename) TestModificationChain {
			return TestModificationChain{
				files: []Filename{a, cExt},
				after: TestModificationsList{
					TestSimpleModification{move(a, bExt)},
					TestSimpleModification{move(cExt, a)},
				},
			}
		})(generateFilename(), generateFilename(".."), generateFilename("..")),
		(func(a, bExt, c Filename) TestModificationChain {
			return TestModificationChain{
				files: []Filename{a},
				after: TestModificationsList{
					TestSimpleModification{move(a, bExt)},
					TestSimpleModification{move(bExt, c)},
				},
			}
		})(generateFilename(), generateFilename(".."), generateFilename()), // group end
		(func(a, bExt, cExt Filename) TestModificationChain {
			return TestModificationChain{
				files: []Filename{a, bExt, cExt}, // MAYBE: find a normal way to test, and remove `a` (or make it optional)
				after: TestModificationsList{
					TestSimpleModification{move(bExt, a)},
					TestOptionalModification{write(a)},
					TestSimpleModification{move(a, cExt)},
					TestSimpleModification{MovementCleanup},
				},
			}
		})(generateFilename(), generateFilename(".."), generateFilename("..")),
		(func(a, b, c Filename) TestModificationChain {
			return TestModificationChain{
				files: []Filename{a, b},
				after: TestModificationsList{
					TestSimpleModification{move(b, c)},
					TestSimpleModification{move(a, b)},
					TestVariantsModification{[]string{
						"",
						write(c),
						write(b),
					}},
				},
			}
		})(generateFilename(), generateFilename(), generateFilename()),
		(func(a, b, c Filename) TestModificationChain {
			return TestModificationChain{
				files: []Filename{a, b, c},
				after: TestModificationsList{
					TestSimpleModification{move(a, b)},
					TestSimpleModification{move(b, c)},
					TestSimpleModification{remove(c)},
				},
				//Deleted{b},
				//Deleted{a},
				//Deleted{c},
			}
		})(generateFilename(), generateFilename(), generateFilename()),
		(func(a, b Filename) TestModificationChain {
			return TestModificationChain{
				files: []Filename{a},
				after: TestModificationsList{
					TestSimpleModification{move(a, b)},
					TestSimpleModification{move(b, a)},
				},
				//Deleted{b},
			}
		})(generateFilename(), generateFilename()),
		(func(a, bExt, c Filename) TestModificationChain {
			return TestModificationChain{
				files: []Filename{a, c},
				after: TestModificationsList{
					TestSimpleModification{move(a, bExt)},
					TestVariantsModification{[]string{
						move(bExt, a),
						move(bExt, c),
					}},
				},
			}
		})(generateFilename(), generateFilename(".."), generateFilename()),
		(func(a, b Filename) TestModificationChain {
			return TestModificationChain{
				files: []Filename{a},
				after: TestModificationsList{
					TestSimpleModification{move(a, b)},
					TestOptionalModification{write(a)},
					TestOptionalModification{write(b)},
					TestOptionalModification{remove(b)},
				},
			}
		})(generateFilename(), generateFilename()),
		(func(a, b, c Filename) TestModificationChain {
			return TestModificationChain{
				files: []Filename{a, c},
				after: TestModificationsList{
					TestSimpleModification{move(a, b)},
					TestOptionalModification{write(a)},
					TestSimpleModification{move(c, b)},
				},
			}
		})(generateFilename(), generateFilename(), generateFilename()),
		(func(a, b Filename) TestModificationChain {
			return TestModificationChain{
				files: []Filename{a},
				after: TestModificationsList{
					TestOptionalModification{remove(a)},
					TestSimpleModification{write(a)},
					TestSimpleModification{move(a, b)},
					TestOptionalModification{write(a)},
					TestOptionalModification{remove(a)},
				},
			}
		})(generateFilename(), generateFilename()),

		(func() TestModificationChain {
			chain := generateFilenamesChain(2)
			a, aParent, b := chain[1], chain[0], generateFilename()

			return TestModificationChain{
				files: []Filename{a},
				after:  TestModificationsList{
					TestSimpleModification{move(aParent, b)},
				},
			}
		})(),
		(func() TestModificationChain {
			chain1 := generateFilenamesChain(2)
			chain2 := generateFilenamesChain(2)
			a, aParent, b, bParent := chain1[1], chain1[0], chain2[1], chain2[0]

			return TestModificationChain{
				files: []Filename{a, b},
				after:  TestModificationsList{
					TestVariantsModification{[]string{
						move(a, b),
						move(a, bParent),
						move(aParent, bParent),
					}},
				},
			}
		})(),
		(func(aFile, bDir, cDir Filename) TestModificationChain {
			chain := generateFilenamesChain(2)
			dDir, dParent := chain[1], chain[0]

			return TestModificationChain{
				files: []Filename{aFile},
				dirs:  []Filename{bDir, cDir, dDir},
				after: TestModificationsList{
					TestOptionalModification{write(aFile)},
					TestSimpleModification{move(aFile, bDir)},
					TestSimpleModification{move(bDir, dDir)},
					TestSimpleModification{move(dParent, cDir)},
				},
			}
		})(generateFilename(), generateFilename(), generateFilename()),

		//(func() TestModificationChain { // TODO: uncomment when fixed
		//	generateFilenames := func(length int) []Filename {
		//		filenames := make([]Filename, 0, length)
		//		for i := 0; i < length; i++ {
		//			filenames = append(filenames, generateFilename(generateFilenamePart()))
		//		}
		//		return filenames
		//	}
		//
		//	nrFilesAfter := 10
		//	modifications := make([]TestModificationInterface, 0, nrFilesAfter)
		//	for i := 0; i < nrFilesAfter; i++ {
		//		modifications = append(
		//			modifications,
		//			TestSimpleModification{write(generateFilename(generateFilenamePart()))},
		//		)
		//	}
		//
		//	return TestModificationChain{
		//		files: generateFilenames(100),
		//		dirs:  generateFilenames(100),
		//		after: modifications,
		//	}
		//})(),
		//(func() TestModificationChain {
		//	nrCases := 100
		//	commands := make([]string, 0, nrCases)
		//	for i := 0; i < nrCases; i++ {
		//		commands = append(commands, write(generateFilename(generateFilenamePart())))
		//		commands = append(commands, mkdir(generateFilename(generateFilenamePart())))
		//	}
		//	return TestModificationChain{
		//		after: TestModificationsList{
		//			TestSimpleModification{strings.Join(commands, ";")},
		//		},
		//	}
		//})(),
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
	filenames := make([]Filename, 0, len(apostrophes))
	for i := 0; i <= len(apostrophes); i++ {
		filenames = append(
			filenames,
			Filename("abc,.;'[]\\<>?\"{}|123`~!@#$%^&*()-=_+ \t\nабв🙂👍❗" + strings.Join(apostrophes[:i], "")),
		)
	}
	for i, filename := range filenames { filenames[i] = "./target/" + filename }
	chains := []func(filename Filename) TestModificationChain{
		func(filename Filename) TestModificationChain {
			return TestModificationChain{after: TestModificationsList{TestSimpleModification{create(filename)}}}
		},
		func(filename Filename) TestModificationChain {
			return TestModificationChain{after: TestModificationsList{TestSimpleModification{write(filename)}}}
		},
		func(filename Filename) TestModificationChain {
			filename2 := filename + "$"
			return TestModificationChain{
				files: []Filename{filename2},
				after: TestModificationsList{TestSimpleModification{move(filename2, filename)}},
			}
		},
		func(filename Filename) TestModificationChain {
			return TestModificationChain{
				files: []Filename{filename},
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
		masterChain.files = append(masterChain.files, chain.files...)
		masterChain.dirs = append(masterChain.dirs, chain.dirs...)
		masterChain.after = append(masterChain.after, simplify(chain.after)...)
	}
	for _, delaySeconds := range delaysMaster {
		chains = append(chains, TestModificationChain{
			files: masterChain.files,
			dirs:  masterChain.dirs,
			after: mergeDelays(masterChain.after, delaySeconds),
		})
	}
	for _, delaySeconds := range delaysBasic {
		for _, chain := range basicChains {
			chains = append(chains, TestModificationChain{
				files: chain.files,
				dirs:  chain.dirs,
				after: mergeDelays(chain.after, delaySeconds),
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
	ErrorCmd        string
	StopOnFail      bool
	IntegrationTest bool
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
		allowEmpty := field.Kind() == reflect.Bool || fieldName == "ErrorCmd"
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
			fmt.Sprintf("%s -M %s -t 'echo done && sleep 420'", sshCmd, testConfig.RemoteAddress),
			func(string) { masterConnectionReady.Done() },
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
			resetCmd = "find . -type f -not -name '.gitignore' -delete && find . -type d -not -name 'target' -delete"
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
	defer func() {
		SUTsDone.Wait()
		reset(testConfig.RemotePath, sandbox, true)
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

	loggers := make([]*Logger, 0, testConfig.NrThreads)
	for i := 0; i < testConfig.NrThreads; i++ {
		loggers = append(loggers, &Logger{
			debug: &InMemoryDebugLogger{formatter: LogFormatter{timestamps: true}},
			error: (func() ErrorLogger {
				if testConfig.ErrorCmd != "" {
					return ComboErrorLogger{[]ErrorLogger{
						ErrorCmdLogger{testConfig.ErrorCmd},
						StdErrLogger{},
					}}
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
			mkdir_ := fmt.Sprintf("mkdir -p %s", targetDir)
			my.RunCommand(sandbox, mkdir_, nil, func(err string) { panic(err) })
			executeRemote(testConfig.RemotePath, mkdir_)

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
					logger:       *logger,
				})
				client.onReady = func() {
					logger.Debug("client.onReady")
					syncing.Unlock()
				}
				go client.Run()
				defer func() {
					Must(client.Close())
					SUTsDone.Done()
				}()
				client.remote.Ready().Wait()
			}

			awaitSync := func() {
				if testConfig.IntegrationTest {
					time.Sleep(time.Duration(testConfig.TimeoutSeconds) * time.Second)
				} else {
					logger.Debug("awaiting sync", my.Trace{}.New().Local())
					synced := *cancellableTimer(
						time.Duration(testConfig.TimeoutSeconds) * time.Second,
						func() {
							my.Dump("stuck logs:")
							logger.Debug("")
							logger.debug.(*InMemoryDebugLogger).collector.Print()
							panic("test failed")
						},
					)
					syncing.Wait()
					synced()
				}
			}

			for scenario := range scenarios {
				(func() {
					my.Dump(scenarioIdx)
					scenarioIdx++ // MAYBE: atomic
				})()
				logger.Debug("scenario", scenario)

				check := func() {
					logger.Debug("check")
					localPath := localTarget
					remotePath := remoteTarget
//					hashCmd := `
//(
// find . -type f -print0 | LC_ALL=C sort -z | xargs -0 -r sha1sum;
// find . \( -type f -o -type d \) -print0 | LC_ALL=C sort -z | xargs -0 -r stat -c '%n %a'
//) | sha1sum
//`
					hashCmd := `
(
 find . -type f -print0 | LC_ALL=C sort -z | xargs -0 -r sha1sum;
 find . -type f -print0 | LC_ALL=C sort -z | xargs -0 -r stat -c '%n %a'
) | sha1sum
`
					var localHash string
					for localHash == "" {
						my.RunCommand(
							localPath,
							hashCmd,
							func(out string) { localHash = out },
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
						logger.Debug("trace", my.Trace{}.New().Local())
						for _, cmd := range []string{
							"find . -type f -print0 | LC_ALL=C sort -z | xargs -0 -r sha1sum",
							//"find . \\( -type f -o -type d \\) -print0 | LC_ALL=C sort -z | xargs -0 stat -c '%n %a'",
							"find . -type f -print0 | LC_ALL=C sort -z | xargs -0 stat -c '%n %a'",
							hashCmd,
							"tree ..",
							//"tree ../..",
							"find . -type f -printf \"%p:\" -exec cat {} \\; | LC_ALL=C sort", // MAYBE: fix `cat`'ing empty files
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
							equal := reflect.DeepEqual(local, remote)
							logger.Debug("equal", equal)
							if !equal {
								var diff []string
								my.RunCommand(
									"",
									fmt.Sprintf(
										"bash -c 'diff <(echo \"%s\") <(echo \"%s\")'",
										strings.Join(local, "\n"),
										strings.Join(remote, "\n"),
									),
									func(out string) { diff = append(diff, out) },
									func(err string) { logger.Error(err) },
								)
								logger.Debug("diff", diff)
							}
						}
						if testConfig.StopOnFail {
							my.Dump("logs:")
							logger.debug.(*InMemoryDebugLogger).collector.Print()
							panic("test failed")
						}
					}
				}

				//scenario.applyTarget(processId)

				for _, dirname := range scenario.dirs {
					logger.Debug("dir.before", dirname)

					my.RunCommand(
						localSandbox,
						mkdir(dirname),
						nil,
						func(err string) { panic(err) },
					)
				}
				for _, filename := range scenario.files {
					logger.Debug("file.before", filename)

					if !testConfig.IntegrationTest { syncing.Lock() }

					my.RunCommand(
						localSandbox,
						write(filename),
						nil,
						func(err string) { panic(err) },
					)
				}
				awaitSync()
				check()

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
				time.Sleep(time.Duration(testConfig.TimeoutSeconds) * time.Second) // TODO: make it normal
				logger.debug.(*InMemoryDebugLogger).collector.Clear()
			}

			wg.Done()
		}(i + 1)
	}
	wg.Wait()
}
