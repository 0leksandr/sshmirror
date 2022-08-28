package main

import (
	"fmt"
	"github.com/0leksandr/my.go"
	"os"
	"testing"
	"time"
)

func TestWatchers(t *testing.T) {
	defer func() { clearDir(getSandbox()) }()

	targetDir := getTargetDir()
	for _, watcher := range []Watcher{
		//FsnotifyWatcher{}.New(targetDir, ""),
		(func() Watcher {
			watcher, err := InotifyWatcher{}.New(
				targetDir,
				"", // MAYBE: test exclude
				Logger{
					debug: NullLogger{},
					error: StdErrLogger{LogFormatter{false}},
				},
			)
			PanicIf(err)
			return watcher
		})(),
	} {
		(func() {
			defer clearDir(targetDir)

			runCommand := func(command string) {
				my.RunCommand(
					targetDir,
					command,
					nil,
					func(err string) { panic(err) },
				)
			}
			assertModification := func(command string, expected Modification) {
				runCommand(command)

				select {
					case modification := <-watcher.Modifications():
						my.AssertEquals(t, modification, expected, watcher, command, modification)
					case <-time.After(10 * time.Millisecond):
						my.Assert(t, expected == nil, watcher, command)
				}
			}

			assertModification(mkdir("aaa"), nil)

			for _, filename := range []Filename{
				"a", // MAYBE: use `generateFilenamePart`
				"aaa/bbb",
			} {
				assertModification(create(filename), Updated{Path{}.New(filename, false)})
				assertModification(move(filename, "../a"), Deleted{Path{}.New(filename, false)})
				assertModification(write(filename), Updated{Path{}.New(filename, false)})
				assertModification(remove(filename), Deleted{Path{}.New(filename, false)})
			}

			assertModification(write("a"), Updated{Path{}.New("a", false)})
			assertModification(
				move("a", "b"),
				Moved{Path{}.New("a", false), Path{}.New("b", false)},
			)
			assertModification(
				move("b", "aaa/c"),
				Moved{Path{}.New("b", false), Path{}.New("aaa/c", false)},
			)
			assertModification(
				move("aaa", "bbb"),
				Moved{Path{}.New("aaa", true), Path{}.New("bbb", true)},
			)
		})()
	}
}

func getSandbox() string {
	currentDir, err := os.Getwd()
	PanicIf(err)
	return fmt.Sprintf("%s/sandbox", currentDir)
}
func getTargetDir() string {
	sandbox := getSandbox()
	targetDir := "target"
	my.RunCommand(
		sandbox,
		fmt.Sprintf("mkdir -p %s", targetDir),
		nil,
		func(err string) { panic(err) },
	)

	return fmt.Sprintf("%s/%s", sandbox, targetDir)
}
func clearDir(dir string) {
	my.RunCommand(
		dir,
		"find . -type f -not -name '.gitignore' -delete && find . -type d -delete",
		nil,
		func(err string) { panic(err) },
	)
}
