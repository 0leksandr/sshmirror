package main

import (
	"fmt"
	"github.com/0leksandr/my.go"
	"os"
	"testing"
	"time"
)

type TestCase struct {
	command  string
	expected Modification
}

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

			for _, testCase := range []TestCase{
				{create("a"), Updated{Path{}.New("a", false)}},
				{write("a"), Updated{Path{}.New("a", false)}},
				{write("b"), Updated{Path{}.New("b", false)}},
				{
					move("b", "c"),
					Moved{Path{}.New("b", false), Path{}.New("c", false)},
				},
				{move("a", "../a"), Deleted{Path{}.New("a", false)}},
				{remove("c"), Deleted{Path{}.New("c", false)}},
			} {
				my.RunCommand(
					targetDir,
					testCase.command,
					nil,
					func(err string) { panic(err) },
				)
				time.Sleep(3 * time.Millisecond)
				my.AssertEquals(t, <-watcher.Modifications(), testCase.expected, watcher, testCase)
			}
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
