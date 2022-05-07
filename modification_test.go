package main

import (
	"github.com/0leksandr/my.go"
	"testing"
)

func TestModificationsQueue_Optimize(t *testing.T) {
	for i, testCase := range basicModificationCases() {
		queue := ModificationsQueue{}
		for _, modification := range testCase.expectedModifications {
			Must(queue.Add(modification))
		}
		Must(queue.Optimize())
		my.Assert(
			t,
			queue.Equals(testCase.expectedQueue),
			i, testCase, testCase.expectedQueue, &queue,
		)
	}
}
