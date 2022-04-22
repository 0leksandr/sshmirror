package main

import (
	"fmt"
	"regexp"
)

const MovementCleanup = "sleep 0.003" // MAYBE: come up with something better

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
func (scenario TestScenario) applyTarget(targetId int) { // TODO: remove
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
	before TestModificationsList // TODO: filenames list
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

type TestModificationCase struct { // MAYBE: rename
	chain                  TestModificationChain
	expectedModifications  []Modification
	expectedUploadingQueue UploadingModificationsQueue
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
func create(filename TestFilename) string { // MAYBE: touch
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

func basicModificationCases() []TestModificationCase {
	return []TestModificationCase{
		(func(a TestFilename, b TestFilename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: TestModificationsList{
						TestSimpleModification{write(a, 10)},
						TestSimpleModification{write(b, 11)},
					},
					after: TestModificationsList{
						TestSimpleModification{remove(a)},
						TestSimpleModification{move(b, a)},
					},
				},
				expectedModifications: []Modification{
					Deleted{filename: string(a)},
					Moved{
						from: string(b),
						to:   string(a),
					},
				},
				expectedUploadingQueue: UploadingModificationsQueue{
					moved: []Moved{
						{
							from: string(b),
							to:   string(a),
						},
					},
				},
			}
		})(generateFilename(true), generateFilename(true)),
		(func(a TestFilename, b TestFilename, cExt TestFilename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
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
						TestSimpleModification{MovementCleanup},
					},
				},
				expectedModifications: []Modification{
					Updated{filename: string(a)},
					Moved{
						from: string(a),
						to:   string(b),
					},
					Deleted{filename: string(b)},
				},
				expectedUploadingQueue: UploadingModificationsQueue{
					deleted: []Deleted{
						{filename: string(a)},
						{filename: string(b)},
					},
				},
			}
		})(generateFilename(true), generateFilename(true), generateFilename(false)),
		(func(a TestFilename, b TestFilename, c TestFilename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: TestModificationsList{
						TestSimpleModification{create(a)},
					},
					after: TestModificationsList{
						TestSimpleModification{move(a, b)},
						TestSimpleModification{move(b, c)},
					},
				},
				expectedModifications: []Modification{
					Moved{
						from: string(a),
						to:   string(b),
					},
					Moved{
						from: string(b),
						to:   string(c),
					},
				},
				expectedUploadingQueue: UploadingModificationsQueue{
					deleted: []Deleted{
						{filename: string(b)},
					},
					moved: []Moved{
						{
							from: string(a),
							to:   string(c),
						},
					},
				},
			}
		})(generateFilename(true), generateFilename(true), generateFilename(true)),
		(func(a TestFilename, b TestFilename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
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
					},
					after: TestModificationsList{
						TestSimpleModification{move(a, b)},
						TestVariantsModification{[]string{
							create(a),
							write(a, 14),
						}},
					},
				},
				expectedModifications: []Modification{
					Moved{
						from: string(a),
						to:   string(b),
					},
					Updated{filename: string(a)},
				},
				expectedUploadingQueue: UploadingModificationsQueue{
					moved: []Moved{
						{
							from: string(a),
							to:   string(b),
						},
					},
					updated: []Updated{
						{filename: string(a)},
					},
				},
			}
		})(generateFilename(true), generateFilename(true)),
		(func(a TestFilename, b TestFilename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
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
					},
					after: TestModificationsList{
						TestSimpleModification{move(a, b)},
						TestSimpleModification{write(b, 13)},
						TestVariantsModification{[]string{
							create(a),
							write(a, 14),
						}},
					},
				},
				expectedModifications: []Modification{
					Moved{
						from: string(a),
						to:   string(b),
					},
					Updated{filename: string(b)},
					Updated{filename: string(a)},
				},
				expectedUploadingQueue: UploadingModificationsQueue{
					updated: []Updated{
						{filename: string(b)}, // MAYBE: ignore order
						{filename: string(a)},
					},
				},
			}
		})(generateFilename(true), generateFilename(true)),
		(func(a TestFilename, b TestFilename, c TestFilename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
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
						TestSimpleModification{write(b, 13)},
						TestSimpleModification{move(c, a)},
					},
				},
				expectedModifications: []Modification{
					Moved{
						from: string(a),
						to:   string(b),
					},
					Updated{filename: string(b)},
					Moved{
						from: string(c),
						to:   string(a),
					},
				},
				expectedUploadingQueue: UploadingModificationsQueue{
					updated: []Updated{
						{filename: string(b)},
					},
					moved: []Moved{
						{
							from: string(c),
							to:   string(a),
						},
					},
				},
			}
		})(generateFilename(true), generateFilename(true), generateFilename(true)),
		(func(a TestFilename, b TestFilename, c TestFilename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
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
				},
				expectedModifications: []Modification{
					Moved{
						from: string(a),
						to:   string(c),
					},
					Moved{
						from: string(b),
						to:   string(a),
					},
					Moved{
						from: string(c),
						to:   string(b),
					},
				},
				expectedUploadingQueue: UploadingModificationsQueue{
					updated: []Updated{
						{filename: string(a)},
						{filename: string(b)},
					},
					deleted: []Deleted{
						{filename: string(c)},
					},
				},
			}
		})(generateFilename(true), generateFilename(true), generateFilename(true)),
		(func(a TestFilename, b TestFilename, cExt TestFilename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
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
				},
				expectedModifications: []Modification{
					Deleted{filename: string(a)},
					Updated{filename: string(b)},
				},
				expectedUploadingQueue: UploadingModificationsQueue{
					updated: []Updated{
						{filename: string(b)},
					},
					deleted: []Deleted{
						{filename: string(a)},
					},
				},
			}
		})(generateFilename(true), generateFilename(true), generateFilename(false)),
		(func(a TestFilename, bExt TestFilename, cExt TestFilename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
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
						TestSimpleModification{MovementCleanup},
					},
				},
				expectedModifications: []Modification{
					Updated{filename: string(a)},
					Updated{filename: string(a)},
					Deleted{filename: string(a)},
				},
				expectedUploadingQueue: UploadingModificationsQueue{
					deleted: []Deleted{
						{filename: string(a)},
					},
				},
			}
		})(generateFilename(true), generateFilename(false), generateFilename(false)),
		(func(a TestFilename, b TestFilename, c TestFilename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
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
					after: TestModificationsList{
						TestSimpleModification{move(b, c)},
						TestSimpleModification{move(a, b)},
					},
				},
				expectedModifications: []Modification{
					Moved{
						from: string(b),
						to:   string(c),
					},
					Moved{
						from: string(a),
						to:   string(b),
					},
				},
				expectedUploadingQueue: UploadingModificationsQueue{
					moved: []Moved{
						{
							from: string(b),
							to:   string(c),
						},
						{
							from: string(a),
							to:   string(b),
						},
					},
				},
			}
		})(generateFilename(true), generateFilename(true), generateFilename(true)),
		(func(a TestFilename, b TestFilename, c TestFilename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: TestModificationsList{
						TestSimpleModification{write(a, 10)},
						TestSimpleModification{write(b, 11)},
					},
					after: TestModificationsList{
						TestSimpleModification{move(b, c)},
						TestSimpleModification{move(a, b)},
						TestSimpleModification{write(c, 12)},
					},
				},
				expectedModifications: []Modification{
					Moved{
						from: string(b),
						to:   string(c),
					},
					Moved{
						from: string(a),
						to:   string(b),
					},
					Updated{filename: string(c)},
				},
				expectedUploadingQueue: UploadingModificationsQueue{
					updated: []Updated{
						{filename: string(c)},
					},
					moved: []Moved{
						{
							from: string(a),
							to:   string(b),
						},
					},
				},
			}
		})(generateFilename(true), generateFilename(true), generateFilename(true)),
		(func(a TestFilename, b TestFilename, c TestFilename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: TestModificationsList{
						TestSimpleModification{write(a, 10)},
						TestSimpleModification{write(b, 11)},
					},
					after: TestModificationsList{
						TestSimpleModification{move(b, c)},
						TestSimpleModification{move(a, b)},
						TestSimpleModification{write(b, 13)},
					},
				},
				expectedModifications: []Modification{
					Moved{
						from: string(b),
						to:   string(c),
					},
					Moved{
						from: string(a),
						to:   string(b),
					},
					Updated{filename: string(b)},
				},
				expectedUploadingQueue: UploadingModificationsQueue{
					deleted: []Deleted{
						{filename: string(a)},
					},
					updated: []Updated{
						{filename: string(b)},
					},
					moved: []Moved{
						{
							from: string(b),
							to:   string(c),
						},
					},
				},
			}
		})(generateFilename(true), generateFilename(true), generateFilename(true)),
		(func(a TestFilename, b TestFilename, c TestFilename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
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
					after: TestModificationsList{
						TestSimpleModification{move(a, b)},
						TestSimpleModification{move(b, c)},
						TestSimpleModification{remove(c)},
					},
				},
				expectedModifications: []Modification{
					Moved{
						from: string(a),
						to:   string(b),
					},
					Moved{
						from: string(b),
						to:   string(c),
					},
					Deleted{filename: string(c)},
				},
				expectedUploadingQueue: UploadingModificationsQueue{
					deleted: []Deleted{
						{filename: string(b)},
						{filename: string(a)},
						{filename: string(c)},
					},
				},
			}
		})(generateFilename(true), generateFilename(true), generateFilename(true)),
		(func(a TestFilename, b TestFilename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: TestModificationsList{
						TestVariantsModification{[]string{
							create(a),
							write(a, 10),
						}},
					},
					after: TestModificationsList{
						TestSimpleModification{move(a, b)},
						TestSimpleModification{move(b, a)},
					},
				},
				expectedModifications: []Modification{
					Moved{
						from: string(a),
						to:   string(b),
					},
					Moved{
						from: string(b),
						to:   string(a),
					},
				},
				expectedUploadingQueue: UploadingModificationsQueue{
					deleted: []Deleted{
						{filename: string(b)},
					},
				},
			}
		})(generateFilename(true), generateFilename(true)),
		(func(a TestFilename, bExt TestFilename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: TestModificationsList{
						TestVariantsModification{[]string{
							create(a),
							write(a, 10),
						}},
					},
					after: TestModificationsList{
						TestSimpleModification{move(a, bExt)},
						TestSimpleModification{move(bExt, a)},
					},
				},
				expectedModifications: []Modification{
					Deleted{filename: string(a)},
					Updated{filename: string(a)},
				},
				expectedUploadingQueue: UploadingModificationsQueue{
					updated: []Updated{
						{filename: string(a)},
					},
				},
			}
		})(generateFilename(true), generateFilename(false)),
		(func(a TestFilename, bExt TestFilename, c TestFilename) TestModificationCase {
			return TestModificationCase{
				chain: TestModificationChain{
					before: TestModificationsList{
						TestVariantsModification{[]string{
							create(a),
							write(a, 10),
						}},
						TestVariantsModification{[]string{
							"",
							create(c),
							write(c, 11),
						}},
					},
					after: TestModificationsList{
						TestSimpleModification{move(a, bExt)},
						TestSimpleModification{move(bExt, c)},
					},
				},
				expectedModifications: []Modification{
					Deleted{filename: string(a)},
					Updated{filename: string(c)},
				},
				expectedUploadingQueue: UploadingModificationsQueue{
					deleted: []Deleted{
						{filename: string(a)},
					},
					updated: []Updated{
						{filename: string(c)},
					},
				},
			}
		})(generateFilename(true), generateFilename(false), generateFilename(true)),
	}
}
