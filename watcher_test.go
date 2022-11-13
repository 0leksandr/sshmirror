package main

import (
	"fmt"
	"github.com/0leksandr/my.go"
	"os"
	"regexp"
	"testing"
	"time"
)

func TestWatchers(t *testing.T) {
	clearSandbox := func() { clearDir(getSandbox()) }
	clearSandbox()
	defer clearSandbox()

	targetDir := getTargetDir()
	exclude := regexp.MustCompile("excluded[12]|. --exclude 3")
	for _, watcher := range []Watcher{
		//FsnotifyWatcher{}.New(targetDir, exclude),
		(func() Watcher {
			watcher, err := InotifyWatcher{}.New(
				targetDir,
				exclude,
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

			assertModification := func(command string, expected ...Modification) {
				my.RunCommand(
					targetDir,
					command,
					nil,
					func(err string) { panic(err) },
				)

				var modifications []Modification
				readModifications: for {
					select {
						case modification := <-watcher.Modifications():
							modifications = append(modifications, modification)
						case <-time.After(10 * time.Millisecond):
							break readModifications
					}
				}
				my.AssertEquals(t, modifications, expected, watcher, command, my.GetTrace(false))
			}

			assertModification(mkdir("a"), Updated{Path{}.New("a")})
			assertModification(remove("a"), Deleted{Path{}.New("a")})
			assertModification(create("a"), Updated{Path{}.New("a")})
			assertModification(move("a", "../a"), Deleted{Path{}.New("a")})
			assertModification(write("a"), Updated{Path{}.New("a")})
			assertModification(remove("a"), Deleted{Path{}.New("a")})

			assertModification(mkdir("aaa/bbb"), Updated{Path{}.New("aaa")})
			assertModification(remove("aaa/bbb"), Deleted{Path{}.New("aaa/bbb")})
			assertModification(create("aaa/bbb"), Updated{Path{}.New("aaa/bbb")})
			assertModification(move("aaa/bbb", "../a"), Deleted{Path{}.New("aaa/bbb")})
			assertModification(write("aaa/bbb"), Updated{Path{}.New("aaa/bbb")})
			assertModification(remove("aaa/bbb"), Deleted{Path{}.New("aaa/bbb")})

			assertModification(write("a"), Updated{Path{}.New("a")})
			assertModification(
				move("a", "b"),
				Moved{Path{}.New("a"), Path{}.New("b")},
			)
			assertModification(
				move("b", "aaa/c"),
				Moved{Path{}.New("b"), Path{}.New("aaa/c")},
			)
			assertModification(
				move("aaa", "bbb"),
				Moved{Path{}.New("aaa"), Path{}.New("bbb")},
			)

			assertModification(write("excluded1"))
			assertModification(write("excluded2/not-excluded"))
			assertModification(write("not-excluded"), Updated{Path{}.New("not-excluded")})
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
		"find . -type f -not -name '.gitignore' -not -name 'test.db' -delete && find . -type d -delete",
		nil,
		func(err string) { panic(err) },
	)
}
