package main

import (
	"github.com/0leksandr/my.go"
	"testing"
	"time"
)

func TestConvertArray(t *testing.T) {
	expected := make([]Modification, 0)
	for i := 0; i < 5; i++ {
		for _, modification := range []Modification{
			Updated{Path{}.New("a")},
			Moved{Path{}.New("b"), Path{}.New("c")},
			Deleted{Path{}.New("d")},
		} {
			expected = append(expected, modification)
		}
	}
	channel := ConvertArray(expected)
	actual := make([]Modification, 0)
	for modification := range channel { actual = append(actual, modification) }
	my.AssertEquals(t, actual, expected)
}
func TestSequentialConnection(t *testing.T) {
	ch1 := make(chan Modification, 5)
	ch2 := make(chan Modification, 5)
	ch3 := make(chan Modification, 5)
	modification1 := Updated{Path{}.New("a")}
	modification2 := Deleted{Path{}.New("b")}
	modification3 := Moved{Path{}.New("c"), Path{}.New("d")}
	modification4 := Updated{Path{}.New("e")}
	go func() {
		ch1 <- modification1
		ch3 <- modification2
		close(ch3)
		ch2 <- modification3
		close(ch2)
		ch1 <- modification4
		close(ch1)
	}()
	actual := make([]Modification, 0)
	channel := SequentialConnection([]<-chan Modification{ch1, ch2, ch3})
	for modification := range channel {
		actual = append(actual, modification)
	}
	my.AssertEquals(t, actual, []Modification{modification1, modification4, modification3, modification2})
}
func TestMultiply(t *testing.T) {
	channel := make(chan Modification)
	modification1 := Updated{Path{}.New("a")}
	modification2 := Deleted{Path{}.New("b")}
	modification3 := Moved{Path{}.New("c"), Path{}.New("d")}
	go func() {
		for _, modification := range []Modification{
			modification1,
			modification2,
			modification3,
		} {
			channel <- modification
		}
	}()
	multiplied := Multiply(channel, 3)
	consumed := make(map[int][]Modification)
	for i, channel2 := range multiplied {
		go func(i int, channel2 <-chan Modification) {
			for modification := range channel2 {
				consumed[i] = append(consumed[i], modification)
			}
		}(i, channel2)
	}
	time.Sleep(100 * time.Millisecond) // MAYBE: something better
	my.AssertEquals(
		t,
		consumed,
		map[int][]Modification{
			0: {modification1, modification2, modification3},
			1: {modification1, modification2, modification3},
			2: {modification1, modification2, modification3},
		},
	)
}
func TestReservoir(t *testing.T) {
	in := make(chan Modification)
	out := Reservoir(in, 10)
	modification1 := Updated{Path{}.New("a")}
	modification2 := Deleted{Path{}.New("b")}
	modification3 := Moved{Path{}.New("c"), Path{}.New("d")}
	in <- modification1
	in <- modification2
	in <- modification3
	close(in)
	result := make([]Modification, 0)
	for modification := range out { result = append(result, modification) }
	my.AssertEquals(t, result, []Modification{modification1, modification2, modification3})
}
