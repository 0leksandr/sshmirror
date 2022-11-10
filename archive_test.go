package main

import (
	"github.com/0leksandr/my.go"
	"os"
	"testing"
	"time"
)

func TestArchive(t *testing.T) {
	currentDir, err := os.Getwd()
	PanicIf(err)
	archive := Archive{}.New(my.DB{}.New(currentDir + "/sandbox/test.db"))
	modifications := []Modification{
		Updated{Path{}.New("a")},
		Deleted{Path{}.New("b")},
		Moved{Path{}.New("c"), Path{}.New("d")},
	}
	now := time.Now()
	archive.Save(ConvertArray(modifications))
	my.AssertEquals(t, archive.Load(now), modifications)
	archive.Delete(time.Unix(0, 0), time.Now())
}
