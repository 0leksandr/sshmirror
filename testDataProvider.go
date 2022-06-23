package main

import (
	"fmt"
)

const MovementCleanup = "sleep 0.003" // MAYBE: come up with something better

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
	before []Filename
	after  []string // MAYBE: type CommandString
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
	before []Filename // MAYBE: split required and optional
	after  TestModificationsList
}
func (chain TestModificationChain) scenarios() []TestScenario {
	variantsAfter := chain.after.commandsVariants()
	scenarios := make([]TestScenario, 0, len(variantsAfter))
	for _, after := range variantsAfter {
		//copyBefore := make([]string, len(before))
		//copy(copyBefore, before)
		//copyAfter := make([]string, len(after))
		//copy(copyAfter, after)

		scenarios = append(scenarios, TestScenario{
			before: chain.before,
			after:  after,
		})
	}
	return scenarios
}

var fileIndex = 0
func generateFilename(inTarget bool) Filename {
	dir := "."
	if inTarget { dir += "/target" }
	fileIndex++
	return Filename(fmt.Sprintf("%s/file-%d", dir, fileIndex))

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
	//return Filename(dir + filename)
}
func create(filename Filename) string { // MAYBE: touch
	return fmt.Sprintf("touch %s", filename.Escaped())
}
var contentIndex = 0
func write(filename Filename) string {
	contentIndex++
	return fmt.Sprintf("echo %d > %s", contentIndex, filename.Escaped())
}
func move(from, to Filename) string {
	return fmt.Sprintf("mv %s %s", from.Escaped(), to.Escaped())
}
func remove(filename Filename) string {
	return fmt.Sprintf("/bin/rm %s", filename.Escaped())
}

func basicModificationChains() []TestModificationChain {
	return []TestModificationChain{
		(func(a Filename) TestModificationChain {
			return TestModificationChain{
				before: []Filename{},
				after: TestModificationsList{
					TestSimpleModification{create(a)},
				},
			}
		})(generateFilename(true)),
		(func(a, b Filename) TestModificationChain {
			return TestModificationChain{
				before: []Filename{a, b},
				after: TestModificationsList{
					TestSimpleModification{remove(a)},
					TestSimpleModification{move(b, a)},
				},
				//Moved{b, a},
			}
		})(generateFilename(true), generateFilename(true)),
		(func(a, b, cExt Filename) TestModificationChain {
			return TestModificationChain{
				before: []Filename{b},
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
		})(generateFilename(true), generateFilename(true), generateFilename(false)),
		(func(a, b, c Filename) TestModificationChain {
			return TestModificationChain{
				before: []Filename{a},
				after: TestModificationsList{
					TestSimpleModification{move(a, b)},
					TestSimpleModification{move(b, c)},
				},
				//Moved{a, c},
				//Deleted{b},
			}
		})(generateFilename(true), generateFilename(true), generateFilename(true)),
		(func(a, b Filename) TestModificationChain {
			return TestModificationChain{
				before: []Filename{a, b},
				after: TestModificationsList{
					TestSimpleModification{move(a, b)},
					TestSimpleModification{write(a)},
				},
			}
		})(generateFilename(true), generateFilename(true)),
		(func(a, b, c Filename) TestModificationChain {
			return TestModificationChain{
				before: []Filename{a, b, c},
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
		})(generateFilename(true), generateFilename(true), generateFilename(true)),
		(func(a, b, c Filename) TestModificationChain {
			return TestModificationChain{
				before: []Filename{a, b, c},
				after: TestModificationsList{
					TestSimpleModification{move(a, c)},
					TestSimpleModification{move(b, a)},
					TestSimpleModification{move(c, b)},
				},
			}
		})(generateFilename(true), generateFilename(true), generateFilename(true)),
		(func(a, b, cExt Filename) TestModificationChain { // group begin: tricky Watcher cases
			return TestModificationChain{
				before: []Filename{a},
				after: TestModificationsList{
					TestSimpleModification{move(a, cExt)},
					TestVariantsModification{[]string{
						create(b),
						write(b),
					}},
				},
			}
		})(generateFilename(true), generateFilename(true), generateFilename(false)),
		(func(a, bExt, cExt Filename) TestModificationChain {
			return TestModificationChain{
				before: []Filename{a, cExt},
				after: TestModificationsList{
					TestSimpleModification{move(a, bExt)},
					TestSimpleModification{move(cExt, a)},
				},
			}
		})(generateFilename(true), generateFilename(false), generateFilename(false)),
		(func(a, bExt, c Filename) TestModificationChain {
			return TestModificationChain{
				before: []Filename{a},
				after: TestModificationsList{
					TestSimpleModification{move(a, bExt)},
					TestSimpleModification{move(bExt, c)},
				},
			}
		})(generateFilename(true), generateFilename(false), generateFilename(true)), // group end
		(func(a, bExt, cExt Filename) TestModificationChain {
			return TestModificationChain{
				before: []Filename{a, bExt, cExt}, // MAYBE: find a normal way to test, and remove `a` (or make it optional)
				after: TestModificationsList{
					TestSimpleModification{move(bExt, a)},
					TestOptionalModification{write(a)},
					TestSimpleModification{move(a, cExt)},
					TestSimpleModification{MovementCleanup},
				},
			}
		})(generateFilename(true), generateFilename(false), generateFilename(false)),
		(func(a, b, c Filename) TestModificationChain {
			return TestModificationChain{
				before: []Filename{a, b},
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
		})(generateFilename(true), generateFilename(true), generateFilename(true)),
		(func(a, b, c Filename) TestModificationChain {
			return TestModificationChain{
				before: []Filename{a, b, c},
				after: TestModificationsList{
					TestSimpleModification{move(a, b)},
					TestSimpleModification{move(b, c)},
					TestSimpleModification{remove(c)},
				},
				//Deleted{b},
				//Deleted{a},
				//Deleted{c},
			}
		})(generateFilename(true), generateFilename(true), generateFilename(true)),
		(func(a, b Filename) TestModificationChain {
			return TestModificationChain{
				before: []Filename{a},
				after: TestModificationsList{
					TestSimpleModification{move(a, b)},
					TestSimpleModification{move(b, a)},
				},
				//Deleted{b},
			}
		})(generateFilename(true), generateFilename(true)),
		(func(a, bExt, c Filename) TestModificationChain {
			return TestModificationChain{
				before: []Filename{a, c},
				after: TestModificationsList{
					TestSimpleModification{move(a, bExt)},
					TestVariantsModification{[]string{
						move(bExt, a),
						move(bExt, c),
					}},
				},
			}
		})(generateFilename(true), generateFilename(false), generateFilename(true)),
		(func(a, b Filename) TestModificationChain {
			return TestModificationChain{
				before: []Filename{a},
				after: TestModificationsList{
					TestSimpleModification{move(a, b)},
					TestOptionalModification{write(a)},
					TestOptionalModification{write(b)},
					TestOptionalModification{remove(b)},
				},
			}
		})(generateFilename(true), generateFilename(true)),
		(func(a, b, c Filename) TestModificationChain {
			return TestModificationChain{
				before: []Filename{a, c},
				after: TestModificationsList{
					TestSimpleModification{move(a, b)},
					TestOptionalModification{write(a)},
					TestSimpleModification{move(c, b)},
				},
			}
		})(generateFilename(true), generateFilename(true), generateFilename(true)),
		(func(a, b Filename) TestModificationChain { // group begin
			return TestModificationChain{
				before: []Filename{a},
				after: TestModificationsList{
					TestOptionalModification{remove(a)},
					TestSimpleModification{write(a)},
					TestSimpleModification{move(a, b)},
					TestOptionalModification{write(a)},
					TestOptionalModification{remove(a)},
				},
			}
		})(generateFilename(true), generateFilename(true)),
	}
}
