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

var fileIndex = 0
func generatePath(inTarget bool) Path {
	dir := "."
	if inTarget { dir += "/target" }
	fileIndex++
	return Path{}.New(
		Filename(fmt.Sprintf("%s/file-%d", dir, fileIndex)),
		false,
	)

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
	//	return generatePath(inTarget)
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
		(func(a Path) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{},
					after: TestModificationsList{
						TestSimpleModification{create(a.original)},
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
		})(generatePath(true)),
		(func(a, b Path) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a.original, b.original},
					after: TestModificationsList{
						TestSimpleModification{remove(a.original)},
						TestSimpleModification{move(b.original, a.original)},
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
		})(generatePath(true), generatePath(true)),
		(func(a, b, cExt Path) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{b.original},
					after: TestModificationsList{
						TestSimpleModification{write(a.original)},
						TestSimpleModification{move(a.original, b.original)},
						TestVariantsModification{[]string{
							remove(b.original),
							move(b.original, cExt.original),
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
		})(generatePath(true), generatePath(true), generatePath(false)),
		(func(a, b, c Path) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a.original},
					after: TestModificationsList{
						TestSimpleModification{move(a.original, b.original)},
						TestSimpleModification{move(b.original, c.original)},
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
		})(generatePath(true), generatePath(true), generatePath(true)),
		(func(a, b Path) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a.original, b.original},
					after: TestModificationsList{
						TestSimpleModification{move(a.original, b.original)},
						TestSimpleModification{write(a.original)},
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
		})(generatePath(true), generatePath(true)),
		(func(a, b Path) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a.original, b.original},
					after: TestModificationsList{
						TestSimpleModification{move(a.original, b.original)},
						TestSimpleModification{write(b.original)},
						TestSimpleModification{write(a.original)},
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
		})(generatePath(true), generatePath(true)),
		(func(a, b, c Path) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a.original, b.original, c.original},
					after: TestModificationsList{
						TestSimpleModification{move(a.original, b.original)},
						TestSimpleModification{write(b.original)},
						TestSimpleModification{move(c.original, a.original)},
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
		})(generatePath(true), generatePath(true), generatePath(true)),
		(func(a, b, c Path) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a.original, b.original, c.original},
					after: TestModificationsList{
						TestSimpleModification{move(a.original, c.original)},
						TestSimpleModification{move(b.original, a.original)},
						TestSimpleModification{move(c.original, b.original)},
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
		})(generatePath(true), generatePath(true), generatePath(true)),
		(func(a, b, cExt Path) TestModificationCase { // group begin: tricky Watcher cases
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a.original},
					after: TestModificationsList{
						TestSimpleModification{move(a.original, cExt.original)},
						TestSimpleModification{create(b.original)},
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
		})(generatePath(true), generatePath(true), generatePath(false)),
		(func(a, bExt, cExt Path) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a.original, cExt.original},
					after: TestModificationsList{
						TestSimpleModification{move(a.original, bExt.original)},
						TestSimpleModification{move(cExt.original, a.original)},
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
		})(generatePath(true), generatePath(false), generatePath(false)),
		(func(a, bExt, c Path) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a.original},
					after: TestModificationsList{
						TestSimpleModification{move(a.original, bExt.original)},
						TestSimpleModification{move(bExt.original, c.original)},
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
		})(generatePath(true), generatePath(false), generatePath(true)), // group end
		(func(a, bExt, cExt Path) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a.original, bExt.original, cExt.original}, // MAYBE: find a normal way to test, and remove `a` (or make it optional)
					after: TestModificationsList{
						TestSimpleModification{move(bExt.original, a.original)},
						TestOptionalModification{write(a.original)},
						TestSimpleModification{move(a.original, cExt.original)},
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
		})(generatePath(true), generatePath(false), generatePath(false)),
		(func(a, b, c Path) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a.original, b.original},
					after: TestModificationsList{
						TestSimpleModification{move(b.original, c.original)},
						TestSimpleModification{move(a.original, b.original)},
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
		})(generatePath(true), generatePath(true), generatePath(true)),
		(func(a, b, c Path) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a.original, b.original},
					after: TestModificationsList{
						TestSimpleModification{move(b.original, c.original)},
						TestSimpleModification{move(a.original, b.original)},
						TestSimpleModification{write(c.original)},
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
		})(generatePath(true), generatePath(true), generatePath(true)),
		(func(a, b, c Path) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a.original, b.original},
					after: TestModificationsList{
						TestSimpleModification{move(b.original, c.original)},
						TestSimpleModification{move(a.original, b.original)},
						TestSimpleModification{write(b.original)},
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
		})(generatePath(true), generatePath(true), generatePath(true)),
		(func(a, b, c Path) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a.original, b.original, c.original},
					after: TestModificationsList{
						TestSimpleModification{move(a.original, b.original)},
						TestSimpleModification{move(b.original, c.original)},
						TestSimpleModification{remove(c.original)},
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
		})(generatePath(true), generatePath(true), generatePath(true)),
		(func(a, b Path) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a.original},
					after: TestModificationsList{
						TestSimpleModification{move(a.original, b.original)},
						TestSimpleModification{move(b.original, a.original)},
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
		})(generatePath(true), generatePath(true)),
		(func(a, bExt Path) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a.original},
					after: TestModificationsList{
						TestSimpleModification{move(a.original, bExt.original)},
						TestSimpleModification{move(bExt.original, a.original)},
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
		})(generatePath(true), generatePath(false)),
		(func(a, bExt, c Path) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a.original, c.original},
					after: TestModificationsList{
						TestSimpleModification{move(a.original, bExt.original)},
						TestSimpleModification{move(bExt.original, c.original)},
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
		})(generatePath(true), generatePath(false), generatePath(true)),
		(func(a, b Path) TestModificationCase { // group begin
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a.original},
					after: TestModificationsList{
						TestSimpleModification{move(a.original, b.original)},
						TestSimpleModification{write(a.original)}, // MAYBE: optional. Split unit and integration tests data
						TestSimpleModification{write(b.original)}, // MAYBE: optional. Split unit and integration tests data
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
		})(generatePath(true), generatePath(true)),
		(func(a, b Path) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a.original},
					after: TestModificationsList{
						TestSimpleModification{move(a.original, b.original)},
						TestSimpleModification{write(a.original)},
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
		})(generatePath(true), generatePath(true)),
		(func(a, b Path) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a.original},
					after: TestModificationsList{
						TestSimpleModification{move(a.original, b.original)},
						TestSimpleModification{write(b.original)},
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
		})(generatePath(true), generatePath(true)), // group end
		(func(a, b Path) TestModificationCase { // group begin
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a.original},
					after: TestModificationsList{
						TestSimpleModification{move(a.original, b.original)},
						TestSimpleModification{write(a.original)}, // MAYBE: optional
						TestSimpleModification{remove(b.original)},
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
		})(generatePath(true), generatePath(true)),
		(func(a, b Path) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a.original},
					after: TestModificationsList{
						TestSimpleModification{move(a.original, b.original)},
						TestSimpleModification{remove(b.original)},
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
		})(generatePath(true), generatePath(true)), // group end
		(func(a, b, c Path) TestModificationCase { // group begin
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a.original, c.original},
					after: TestModificationsList{
						TestSimpleModification{move(a.original, b.original)},
						TestSimpleModification{write(a.original)}, // MAYBE: optional
						TestSimpleModification{move(c.original, b.original)},
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
		})(generatePath(true), generatePath(true), generatePath(true)),
		(func(a, b, c Path) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a.original, c.original},
					after: TestModificationsList{
						TestSimpleModification{move(a.original, b.original)},
						TestSimpleModification{move(c.original, b.original)},
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
		})(generatePath(true), generatePath(true), generatePath(true)), // group end
		(func(a, b Path) TestModificationCase { // group begin
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a.original},
					after: TestModificationsList{
						TestSimpleModification{remove(a.original)}, // MAYBE: optional
						TestSimpleModification{write(a.original)}, // MAYBE: optional
						TestSimpleModification{move(a.original, b.original)},
						TestSimpleModification{write(a.original)}, // MAYBE: optional
						TestSimpleModification{remove(a.original)}, // MAYBE: optional
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
		})(generatePath(true), generatePath(true)),
		(func(a, b Path) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{},
					after: TestModificationsList{
						TestSimpleModification{write(a.original)},
						TestSimpleModification{move(a.original, b.original)},
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
		})(generatePath(true), generatePath(true)),
		(func(a, b Path) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a.original},
					after: TestModificationsList{
						TestSimpleModification{remove(a.original)},
						TestSimpleModification{write(a.original)},
						TestSimpleModification{move(a.original, b.original)},
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
		})(generatePath(true), generatePath(true)),
		(func(a, b Path) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: []Filename{a.original},
					after: TestModificationsList{
						TestSimpleModification{move(a.original, b.original)},
						TestSimpleModification{write(a.original)},
						TestSimpleModification{remove(a.original)},
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
		})(generatePath(true), generatePath(true)), // group end
	}
}
