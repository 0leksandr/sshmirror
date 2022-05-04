package main

import (
	"github.com/0leksandr/my.go"
	"testing"
)

func TestModificationsQueue_Flush(t *testing.T) {
	for i, testCase := range basicModificationCases() {
		queue := ModificationsQueue{}
		for _, modification := range testCase.expectedModifications {
			Must(queue.Add(modification))
		}
		uploadingModificationsQueue, err := queue.Flush()
		PanicIf(err)
		my.Assert(
			t,
			uploadingModificationsQueue.Equals(testCase.expectedUploadingQueue),
			i, testCase, testCase.expectedUploadingQueue, uploadingModificationsQueue,
		)
	}
}
