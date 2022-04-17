package main

import (
	"errors"
	"os"
)

type Modification interface {
	Join(queue *ModificationsQueue) error
}
// problem with created+updated: impossible to determine which one of the two is a moved file. I.e. a file was moved
// to a new location. Is it created or updated?
// TODO: return `Created`, and improve created + instantly deleted cases

type Updated struct { // any file, that must be uploaded // MAYBE: Written
	filename string
}
type Deleted struct { // existing file, that was deleted
	filename string
}
type Moved struct { // existing file, that was moved to a new location
	from string
	to   string
}
func (updated Updated) Join(queue *ModificationsQueue) error {
	addToQueue := true
	for _, previouslyUpdated := range queue.updated {
		if previouslyUpdated.filename == updated.filename {
			addToQueue = false
		}
	}
	for i, moved := range queue.moved {
		if moved.to == updated.filename {
			queue.removeMoved(i)
			if !queue.HasModifications(moved.from) {
				if err := queue.Add(Deleted{filename: moved.from}); err != nil { return err }
			}
		}
	}
	for i, deleted := range queue.deleted {
		if deleted.filename == updated.filename {
			queue.removeDeleted(i)
		}
	}
	if addToQueue { queue.updated = append(queue.updated, updated) }
	return nil
}
func (deleted Deleted) Join(queue *ModificationsQueue) error {
	for i, updated := range queue.updated {
		if updated.filename == deleted.filename {
			queue.removeUpdated(i)
		}
	}
	for i, moved := range queue.moved {
		if moved.from == deleted.filename {
			return errors.New("moved from, then deleted")
		}
		if moved.to == deleted.filename {
			queue.removeMoved(i)
			if !queue.HasModifications(moved.from) {
				if err := queue.Add(Deleted{filename: moved.from}); err != nil { return err }
			}
		}
	}
	for _, previouslyDeleted := range queue.deleted {
		if previouslyDeleted.filename == deleted.filename {
			return errors.New("deleted, then deleted")
		}
	}
	queue.deleted = append(queue.deleted, deleted)
	return nil
}
func (moved Moved) Join(queue *ModificationsQueue) error {
	if moved.from == moved.to { return errors.New("moving to same location") }

	addToQueue := true
	for _, deleted := range queue.deleted { // before the next block, because else will be overwritten
		if deleted.filename == moved.from {
			return errors.New("deleted, then moved from")
		}
	}

	for _, updated := range queue.updated {
		if updated.filename == moved.from {
			if err := queue.Add(Deleted{filename: moved.from}); err != nil { return err }
			if err := queue.Add(Updated{filename: moved.to}); err != nil { return err }
			addToQueue = false
		}
	}
	for i, updated := range queue.updated {
		if updated.filename == moved.to {
			queue.removeUpdated(i)
		}
	}

	for i, previouslyMoved := range queue.moved {
		if previouslyMoved.from == moved.from {
			return errors.New("moved from, then moved from")
		}
		if previouslyMoved.from == moved.to {
			// TODO: think about
		}
		if previouslyMoved.to == moved.from {
			queue.moved[i].to = moved.to
			if err := queue.Add(Deleted{filename: moved.from}); err != nil { return err }
			addToQueue = false
		}
		if previouslyMoved.to == moved.to {
			queue.removeMoved(i)
			if !queue.HasModifications(previouslyMoved.from) {
				if err := queue.Add(Deleted{filename: previouslyMoved.from}); err != nil { return err }
			}
		}
	}
	if addToQueue { queue.moved = append(queue.moved, moved) }
	return nil
}

type ModificationsQueue struct { // TODO: atomic
	updated []Updated
	deleted []Deleted
	moved   []Moved
}
func (queue *ModificationsQueue) Add(modification Modification) error {
	return modification.Join(queue)
}
func (queue *ModificationsQueue) HasModifications(filename string) bool {
	for _, updated := range queue.updated {
		if updated.filename == filename { return true }
	}
	for _, deleted := range queue.deleted {
		if deleted.filename == filename { return true }
	}
	for _, moved := range queue.moved {
		if moved.to == filename { return true }
	}
	return false
}
func (queue *ModificationsQueue) Apply(client RemoteClient) {
	if len(queue.deleted) > 0 {
		deletedFilenames := make([]string, 0, len(queue.deleted))
		for _, deleted := range queue.deleted { deletedFilenames = append(deletedFilenames, deleted.filename) }
		client.Delete(deletedFilenames)
	}

	for _, moved := range queue.moved {
		client.Move(moved.from, moved.to)
	}

	if len(queue.updated) > 0 {
		updatedFilenames := make([]string, 0, len(queue.updated))
		for _, updated := range queue.updated { updatedFilenames = append(updatedFilenames, updated.filename) }
		client.Upload(updatedFilenames)
	}
}
func (queue *ModificationsQueue) Copy() ModificationsQueue {
	updated := make([]Updated, len(queue.updated))
	deleted := make([]Deleted, len(queue.deleted))
	moved := make([]Moved, len(queue.moved))
	copy(updated, queue.updated)
	copy(deleted, queue.deleted)
	copy(moved, queue.moved)

	return ModificationsQueue{
		updated: updated,
		deleted: deleted,
		moved:   moved,
	}
}
func (queue *ModificationsQueue) Optimize(localDir string) error {
	// check for circular move
	removedCircular := true
	for removedCircular {
		var err error
		removedCircular, err = (func() (bool, error) {
			for i := 0; i < len(queue.moved); i++ {
				movedI := queue.moved[i]
				for j := i+1; j < len(queue.moved); j++ {
					movedJ := queue.moved[j]
					if movedI.from == movedJ.to && movedI.to == movedJ.from {
						queue.removeMoved(j)
						queue.removeMoved(i)
						// MAYBE: something smarter
						var err2 error
						for _, filename := range [2]string{movedI.from , movedJ.from} {
							if err3 := queue.Add(Updated{filename: filename}); err3 != nil { err2 = err3 }
						}
						return true, err2
					}
				}
			}
			return false, nil
		})()
		if err != nil { return err }
	}

	// false moves
	// TODO: is this specific for fsnotify? Move there
	fileEmpty := func(filename string) (bool, error) {
		fileInfo, err := os.Stat(localDir + string(os.PathSeparator) + filename)
		if err == nil {
			return fileInfo.Size() == 0, nil
		} else {
			return false, err
		}
	}
	for i := 0; i < len(queue.moved); i++ {
		moved := queue.moved[i]
		empty, err := fileEmpty(moved.to)
		if err != nil { return err }
		if empty {
			queue.removeMoved(i)
			i--
			if err2 := queue.Add(Updated{filename: moved.to}); err2 != nil { return err2 }
			if !queue.HasModifications(moved.from) {
				if err2 := queue.Add(Deleted{filename: moved.from}); err2 != nil { return err2 }
			}
		}
	}

	return nil
}
func (queue *ModificationsQueue) IsEmpty() bool {
	return len(queue.updated) == 0 &&
		len(queue.deleted) == 0 &&
		len(queue.moved) == 0
}
func (queue *ModificationsQueue) removeUpdated(i int) {
	last := len(queue.updated) - 1
	if i != last { queue.updated[i] = queue.updated[last] }
	queue.updated = queue.updated[:last]
}
func (queue *ModificationsQueue) removeDeleted(i int) {
	last := len(queue.deleted) - 1
	if i != last { queue.deleted[i] = queue.deleted[last] }
	queue.deleted = queue.deleted[:last]
}
func (queue *ModificationsQueue) removeMoved(i int) {
	last := len(queue.moved) - 1
	if i != last { queue.moved[i] = queue.moved[last] }
	queue.moved = queue.moved[:last]
}
