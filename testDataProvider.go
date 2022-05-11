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

type TestModificationCase struct { // MAYBE: rename
	chain                 TestModificationChain
	expectedModifications []Modification
	expectedQueue         *ModificationsQueue
}

type TestModificationCasesGroup []TestModificationCase

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

func basicModificationCases() []TestModificationCase {
	return []TestModificationCase{
		(func(a Filename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{},
					after: TestModificationsList{
						TestSimpleModification{create(a)},
					},
				},
				expectedModifications: []Modification{
					Updated{a},
				},
				expectedQueue: &ModificationsQueue{
					updated: []Updated{
						{a},
					},
				},
			}
		})(generateFilename(true)),
		(func(a, b Filename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a, b},
					after: TestModificationsList{
						TestSimpleModification{remove(a)},
						TestSimpleModification{move(b, a)},
					},
				},
				expectedModifications: []Modification{
					Deleted{a},
					Moved{b, a},
				},
				expectedQueue: &ModificationsQueue{
					moved: []Moved{
						{b, a},
					},
				},
			}
		})(generateFilename(true), generateFilename(true)),
		(func(a, b, cExt Filename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
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
				},
				expectedModifications: []Modification{
					Updated{a},
					Moved{a, b},
					Deleted{b},
				},
				expectedQueue: &ModificationsQueue{
					deleted: []Deleted{
						{a},
						{b},
					},
				},
			}
		})(generateFilename(true), generateFilename(true), generateFilename(false)),
		(func(a, b, c Filename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a},
					after: TestModificationsList{
						TestSimpleModification{move(a, b)},
						TestSimpleModification{move(b, c)},
					},
				},
				expectedModifications: []Modification{
					Moved{a, b},
					Moved{b, c},
				},
				expectedQueue: &ModificationsQueue{
					deleted: []Deleted{
						{b},
					},
					moved: []Moved{
						{a, c},
					},
				},
			}
		})(generateFilename(true), generateFilename(true), generateFilename(true)),
		(func(a, b Filename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a, b},
					after: TestModificationsList{
						TestSimpleModification{move(a, b)},
						TestSimpleModification{write(a)},
					},
				},
				expectedModifications: []Modification{
					Moved{a, b},
					Updated{a},
				},
				expectedQueue: &ModificationsQueue{
					moved: []Moved{
						{a, b},
					},
					updated: []Updated{
						{a},
					},
				},
			}
		})(generateFilename(true), generateFilename(true)),
		(func(a, b Filename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a, b},
					after: TestModificationsList{
						TestSimpleModification{move(a, b)},
						TestSimpleModification{write(b)},
						TestSimpleModification{write(a)},
					},
				},
				expectedModifications: []Modification{
					Moved{a, b},
					Updated{b},
					Updated{a},
				},
				expectedQueue: &ModificationsQueue{
					updated: []Updated{
						{b}, // MAYBE: ignore order
						{a},
					},
				},
			}
		})(generateFilename(true), generateFilename(true)),
		(func(a, b, c Filename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a, b, c},
					after: TestModificationsList{
						TestSimpleModification{move(a, b)},
						TestSimpleModification{write(b)},
						TestSimpleModification{move(c, a)},
					},
				},
				expectedModifications: []Modification{
					Moved{a, b},
					Updated{b},
					Moved{c, a},
				},
				expectedQueue: &ModificationsQueue{
					updated: []Updated{
						{b},
					},
					moved: []Moved{
						{c, a},
					},
				},
			}
		})(generateFilename(true), generateFilename(true), generateFilename(true)),
		(func(a, b, c Filename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a, b, c},
					after: TestModificationsList{
						TestSimpleModification{move(a, c)},
						TestSimpleModification{move(b, a)},
						TestSimpleModification{move(c, b)},
					},
				},
				expectedModifications: []Modification{
					Moved{a, c},
					Moved{b, a},
					Moved{c, b},
				},
				expectedQueue: &ModificationsQueue{
					updated: []Updated{
						{a},
						{b},
					},
					deleted: []Deleted{
						{c},
					},
				},
			}
		})(generateFilename(true), generateFilename(true), generateFilename(true)),
		(func(a, b, cExt Filename) TestModificationCase { // group begin: tricky Watcher cases
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a},
					after: TestModificationsList{
						TestSimpleModification{move(a, cExt)},
						TestSimpleModification{create(b)},
					},
				},
				expectedModifications: []Modification{
					Deleted{a},
					Updated{b},
				},
				expectedQueue: &ModificationsQueue{
					updated: []Updated{
						{b},
					},
					deleted: []Deleted{
						{a},
					},
				},
			}
		})(generateFilename(true), generateFilename(true), generateFilename(false)),
		(func(a, bExt, cExt Filename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a, cExt},
					after: TestModificationsList{
						TestSimpleModification{move(a, bExt)},
						TestSimpleModification{move(cExt, a)},
					},
				},
				expectedModifications: []Modification{
					Deleted{a},
					Updated{a},
				},
				expectedQueue: &ModificationsQueue{
					updated: []Updated{
						{a},
					},
				},
			}
		})(generateFilename(true), generateFilename(false), generateFilename(false)),
		(func(a, bExt, c Filename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a},
					after: TestModificationsList{
						TestSimpleModification{move(a, bExt)},
						TestSimpleModification{move(bExt, c)},
					},
				},
				expectedModifications: []Modification{
					Deleted{a},
					Updated{c},
				},
				expectedQueue: &ModificationsQueue{
					updated: []Updated{
						{c},
					},
					deleted: []Deleted{
						{a},
					},
				},
			}
		})(generateFilename(true), generateFilename(false), generateFilename(true)), // group end
		(func(a, bExt, cExt Filename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a, bExt, cExt}, // MAYBE: find a normal way to test, and remove `a` (or make it optional)
					after: TestModificationsList{
						TestSimpleModification{move(bExt, a)},
						TestOptionalModification{write(a)},
						TestSimpleModification{move(a, cExt)},
						TestSimpleModification{MovementCleanup},
					},
				},
				expectedModifications: []Modification{
					Updated{a},
					Updated{a},
					Deleted{a},
				},
				expectedQueue: &ModificationsQueue{
					deleted: []Deleted{
						{a},
					},
				},
			}
		})(generateFilename(true), generateFilename(false), generateFilename(false)),
		(func(a, b, c Filename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a, b},
					after: TestModificationsList{
						TestSimpleModification{move(b, c)},
						TestSimpleModification{move(a, b)},
					},
				},
				expectedModifications: []Modification{
					Moved{b, c},
					Moved{a, b},
				},
				expectedQueue: &ModificationsQueue{
					moved: []Moved{
						{b, c},
						{a, b},
					},
				},
			}
		})(generateFilename(true), generateFilename(true), generateFilename(true)),
		(func(a, b, c Filename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a, b},
					after: TestModificationsList{
						TestSimpleModification{move(b, c)},
						TestSimpleModification{move(a, b)},
						TestSimpleModification{write(c)},
					},
				},
				expectedModifications: []Modification{
					Moved{b, c},
					Moved{a, b},
					Updated{c},
				},
				expectedQueue: &ModificationsQueue{
					updated: []Updated{
						{c},
					},
					moved: []Moved{
						{a, b},
					},
				},
			}
		})(generateFilename(true), generateFilename(true), generateFilename(true)),
		(func(a, b, c Filename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a, b},
					after: TestModificationsList{
						TestSimpleModification{move(b, c)},
						TestSimpleModification{move(a, b)},
						TestSimpleModification{write(b)},
					},
				},
				expectedModifications: []Modification{
					Moved{b, c},
					Moved{a, b},
					Updated{b},
				},
				expectedQueue: &ModificationsQueue{
					deleted: []Deleted{
						{a},
					},
					updated: []Updated{
						{b},
					},
					moved: []Moved{
						{b, c},
					},
				},
			}
		})(generateFilename(true), generateFilename(true), generateFilename(true)),
		(func(a, b, c Filename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a, b, c},
					after: TestModificationsList{
						TestSimpleModification{move(a, b)},
						TestSimpleModification{move(b, c)},
						TestSimpleModification{remove(c)},
					},
				},
				expectedModifications: []Modification{
					Moved{a, b},
					Moved{b, c},
					Deleted{c},
				},
				expectedQueue: &ModificationsQueue{
					deleted: []Deleted{
						{b},
						{a},
						{c},
					},
				},
			}
		})(generateFilename(true), generateFilename(true), generateFilename(true)),
		(func(a, b Filename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a},
					after: TestModificationsList{
						TestSimpleModification{move(a, b)},
						TestSimpleModification{move(b, a)},
					},
				},
				expectedModifications: []Modification{
					Moved{a, b},
					Moved{b, a},
				},
				expectedQueue: &ModificationsQueue{
					deleted: []Deleted{
						{b},
					},
				},
			}
		})(generateFilename(true), generateFilename(true)),
		(func(a, bExt Filename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a},
					after: TestModificationsList{
						TestSimpleModification{move(a, bExt)},
						TestSimpleModification{move(bExt, a)},
					},
				},
				expectedModifications: []Modification{
					Deleted{a},
					Updated{a},
				},
				expectedQueue: &ModificationsQueue{
					updated: []Updated{
						{a},
					},
				},
			}
		})(generateFilename(true), generateFilename(false)),
		(func(a, bExt, c Filename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a, c},
					after: TestModificationsList{
						TestSimpleModification{move(a, bExt)},
						TestSimpleModification{move(bExt, c)},
					},
				},
				expectedModifications: []Modification{
					Deleted{a},
					Updated{c},
				},
				expectedQueue: &ModificationsQueue{
					deleted: []Deleted{
						{a},
					},
					updated: []Updated{
						{c},
					},
				},
			}
		})(generateFilename(true), generateFilename(false), generateFilename(true)),
		(func(a, b Filename) TestModificationCase { // group begin
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a},
					after: TestModificationsList{
						TestSimpleModification{move(a, b)},
						TestSimpleModification{write(a)}, // MAYBE: optional. Split unit and integration tests data
						TestSimpleModification{write(b)}, // MAYBE: optional. Split unit and integration tests data
					},
				},
				expectedModifications: []Modification{
					Moved{a, b},
					Updated{a},
					Updated{b},
				},
				expectedQueue: &ModificationsQueue{
					updated: []Updated{
						{a},
						{b},
					},
				},
			}
		})(generateFilename(true), generateFilename(true)),
		(func(a, b Filename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a},
					after: TestModificationsList{
						TestSimpleModification{move(a, b)},
						TestSimpleModification{write(a)},
					},
				},
				expectedModifications: []Modification{
					Moved{a, b},
					Updated{a},
				},
				expectedQueue: &ModificationsQueue{
					moved: []Moved{
						{a, b},
					},
					updated: []Updated{
						{a},
					},
				},
			}
		})(generateFilename(true), generateFilename(true)),
		(func(a, b Filename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a},
					after: TestModificationsList{
						TestSimpleModification{move(a, b)},
						TestSimpleModification{write(b)},
					},
				},
				expectedModifications: []Modification{
					Moved{a, b},
					Updated{b},
				},
				expectedQueue: &ModificationsQueue{
					updated: []Updated{
						{b},
					},
					deleted: []Deleted{
						{a},
					},
				},
			}
		})(generateFilename(true), generateFilename(true)), // group end
		(func(a, b Filename) TestModificationCase { // group begin
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a},
					after: TestModificationsList{
						TestSimpleModification{move(a, b)},
						TestSimpleModification{write(a)}, // MAYBE: optional
						TestSimpleModification{remove(b)},
					},
				},
				expectedModifications: []Modification{
					Moved{a, b},
					Updated{a},
					Deleted{b},
				},
				expectedQueue: &ModificationsQueue{
					updated: []Updated{
						{a},
					},
					deleted: []Deleted{
						{b},
					},
				},
			}
		})(generateFilename(true), generateFilename(true)),
		(func(a, b Filename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a},
					after: TestModificationsList{
						TestSimpleModification{move(a, b)},
						TestSimpleModification{remove(b)},
					},
				},
				expectedModifications: []Modification{
					Moved{a, b},
					Deleted{b},
				},
				expectedQueue: &ModificationsQueue{
					deleted: []Deleted{
						{a},
						{b},
					},
				},
			}
		})(generateFilename(true), generateFilename(true)), // group end
		(func(a, b, c Filename) TestModificationCase { // group begin
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a, c},
					after: TestModificationsList{
						TestSimpleModification{move(a, b)},
						TestSimpleModification{write(a)}, // MAYBE: optional
						TestSimpleModification{move(c, b)},
					},
				},
				expectedModifications: []Modification{
					Moved{a, b},
					Updated{a},
					Moved{c, b},
				},
				expectedQueue: &ModificationsQueue{
					updated: []Updated{
						{a},
					},
					moved: []Moved{
						{c, b},
					},
				},
			}
		})(generateFilename(true), generateFilename(true), generateFilename(true)),
		(func(a, b, c Filename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a, c},
					after: TestModificationsList{
						TestSimpleModification{move(a, b)},
						TestSimpleModification{move(c, b)},
					},
				},
				expectedModifications: []Modification{
					Moved{a, b},
					Moved{c, b},
				},
				expectedQueue: &ModificationsQueue{
					deleted: []Deleted{
						{a},
					},
					moved: []Moved{
						{c, b},
					},
				},
			}
		})(generateFilename(true), generateFilename(true), generateFilename(true)), // group end
		(func(a, b Filename) TestModificationCase { // group begin
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a},
					after: TestModificationsList{
						TestSimpleModification{remove(a)}, // MAYBE: optional
						TestSimpleModification{write(a)}, // MAYBE: optional
						TestSimpleModification{move(a, b)},
						TestSimpleModification{write(a)}, // MAYBE: optional
						TestSimpleModification{remove(a)}, // MAYBE: optional
					},
				},
				expectedModifications: []Modification{
					Deleted{a},
					Updated{a},
					Moved{a, b},
					Updated{a},
					Deleted{a},
				},
				expectedQueue: &ModificationsQueue{
					updated: []Updated{
						{b},
					},
					deleted: []Deleted{
						{a},
					},
				},
			}
		})(generateFilename(true), generateFilename(true)),
		(func(a, b Filename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{},
					after: TestModificationsList{
						TestSimpleModification{write(a)},
						TestSimpleModification{move(a, b)},
					},
				},
				expectedModifications: []Modification{
					Updated{a},
					Moved{a, b},
				},
				expectedQueue: &ModificationsQueue{
					updated: []Updated{
						{b},
					},
					deleted: []Deleted{
						{a},
					},
				},
			}
		})(generateFilename(true), generateFilename(true)),
		(func(a, b Filename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a},
					after: TestModificationsList{
						TestSimpleModification{remove(a)},
						TestSimpleModification{write(a)},
						TestSimpleModification{move(a, b)},
					},
				},
				expectedModifications: []Modification{
					Deleted{a},
					Updated{a},
					Moved{a, b},
				},
				expectedQueue: &ModificationsQueue{
					updated: []Updated{
						{b},
					},
					deleted: []Deleted{
						{a},
					},
				},
			}
		})(generateFilename(true), generateFilename(true)),
		(func(a, b Filename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a},
					after: TestModificationsList{
						TestSimpleModification{move(a, b)},
						TestSimpleModification{write(a)},
						TestSimpleModification{remove(a)},
					},
				},
				expectedModifications: []Modification{
					Moved{a, b},
					Updated{a},
					Deleted{a},
				},
				expectedQueue: &ModificationsQueue{
					deleted: []Deleted{
						{a}, // MAYBE: fix
					},
					moved: []Moved{
						{a, b},
					},
				},
			}
		})(generateFilename(true), generateFilename(true)), // group end
	}
}
