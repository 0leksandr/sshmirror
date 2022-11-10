package main

import (
	"sync"
)

func ConvertArray(modifications []Modification) <-chan Modification {
	channel := make(chan Modification)
	go func() {
		for _, modification := range modifications {
			channel <- modification
		}
		close(channel)
	}()
	return channel
}
func SequentialConnection(channels []<-chan Modification) <-chan Modification {
	result := make(chan Modification)
	go func() {
		for _, channel := range channels {
			for modification := range channel {
				result <- modification
			}
		}
		close(result)
	}()
	return result
}
func Multiply(channel <-chan Modification, nr int) []<-chan Modification { // MAYBE: rename
	channels := make([]chan Modification, 0, nr)
	for i := 0; i < nr; i++ { channels = append(channels, make(chan Modification)) }
	go func() {
		for modification := range channel {
			for _, ch := range channels {
				ch <- modification
			}
		}
		for _, ch := range channels { close(ch) }
	}()

	outChannels := make([]<-chan Modification, 0, len(channels))
	for _, ch := range channels { outChannels = append(outChannels, ch) }
	return outChannels
}
func Reservoir(in <-chan Modification, size int) <-chan Modification {
	out := make(chan Modification)
	reservoir := make([]Modification, 0, size)
	var draining Locker
	draining.Lock()
	var mx sync.Mutex // accessing reservoir or draining state
	inClosed := false
	go func() {
		for modification := range in {
			mx.Lock()
			reservoir = append(reservoir, modification) // MAYBE: allow realtime sending if reservoir is empty
			if len(reservoir) == size {
				// PRIORITY: use Archive as a fallback
			}
			draining.Unlock()
			mx.Unlock()
		}
		mx.Lock()
		inClosed = true
		draining.Unlock()
		mx.Unlock()
	}()
	go func() {
		for {
			draining.Wait()
			for {
				mx.Lock()
				if len(reservoir) > 0 {
					modification := reservoir[0]
					reservoir = reservoir[1:]
					mx.Unlock()
					out <- modification
				} else {
					if inClosed {
						close(out)
						return
					}
					draining.Lock()
					mx.Unlock()
					break
				}
			}
		}
	}()

	return out
}
