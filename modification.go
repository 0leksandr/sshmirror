package main

import (
	"errors"
	"sync"
)

type Modification interface {
	Join(queue *ModificationsQueue) error
}
// problem with created+updated: impossible to determine which one of the two is a moved file. I.e. a file was moved
// to a new location. Is it created or updated?
// TODO: return `Created`, and improve created + instantly deleted cases

type Updated struct { // any file, that must be uploaded // MAYBE: Written
	filename Filename
}
type Deleted struct { // existing file, that was deleted
	filename Filename
}
type Moved struct { // existing file, that was moved to a new location
	from Filename
	to   Filename
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
	for i, deleted := range queue.deleted { // before the next block, because else will be overwritten
		if deleted.filename == moved.from {
			return errors.New("deleted, then moved from")
		}

		if deleted.filename == moved.to {
			queue.removeDeleted(i)
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
			//triangularMove := false // TODO: uncomment and test modifying files within triangle
			//for i2, previouslyMoved2 := range queue.moved {
			//	if previouslyMoved2.from == moved.to {
			//		if i != i2 { triangularMove = true }
			//	}
			//}
			//if !triangularMove {
			//}
			if previouslyMoved.from == moved.to {
				queue.removeMoved(i)
			} else {
				queue.moved[i].to = moved.to
			}
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

type ModificationsQueue struct {
	// MAYBE: map filename: modification. For moved, key is moved.to
	updated []Updated
	deleted []Deleted
	moved   []Moved
	mutex   sync.Mutex
}
func (queue *ModificationsQueue) AtomicAdd(modification Modification) error {
	queue.mutex.Lock()
	defer queue.mutex.Unlock()

	return queue.Add(modification)
}
func (queue *ModificationsQueue) Add(modification Modification) error {
	return modification.Join(queue)
}
func (queue *ModificationsQueue) HasModifications(filename Filename) bool {
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
func (queue *ModificationsQueue) IsEmpty() bool {
	queue.mutex.Lock()
	defer queue.mutex.Unlock()

	return len(queue.updated) == 0 &&
		len(queue.deleted) == 0 &&
		len(queue.moved) == 0
}
func (queue *ModificationsQueue) Flush() (UploadingModificationsQueue, error) {
	if err := queue.optimize(); err != nil { return UploadingModificationsQueue{}, err }

	uploadingModificationsQueue := UploadingModificationsQueue{
		updated: queue.updated,
		deleted: queue.deleted,
		moved:   queue.moved,
	}
	queue.updated = []Updated{}
	queue.deleted = []Deleted{}
	queue.moved = []Moved{}

	return uploadingModificationsQueue, nil
}
func (queue *ModificationsQueue) Copy() *ModificationsQueue {
	updated := make([]Updated, len(queue.updated))
	deleted := make([]Deleted, len(queue.deleted))
	moved := make([]Moved, len(queue.moved))
	copy(updated, queue.updated)
	copy(deleted, queue.deleted)
	copy(moved, queue.moved)

	return &ModificationsQueue{
		updated: updated,
		deleted: deleted,
		moved:   moved,
	}
}
func (queue *ModificationsQueue) Merge(other *ModificationsQueue) error {
	for _, moved := range other.moved {
		if err := queue.Add(moved); err != nil { return err }
	}
	for _, deleted := range other.deleted {
		if err := queue.Add(deleted); err != nil { return err }
	}
	for _, updated := range other.updated {
		if err := queue.Add(updated); err != nil { return err }
	}
	return nil
}
func (queue *ModificationsQueue) optimize() error {
	// check for circular move
	removedCircular := true // TODO: remove after triangular move is uncommented
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
						// THINK: something smarter
						var err2 error
						for _, filename := range [2]Filename{movedI.from , movedJ.from} {
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

	//// false moves
	//// TODO: is this specific for fsnotify? Move there
	//fileEmpty := func(filename string) (bool, error) {
	//	fileInfo, err := os.Stat(localDir + string(os.PathSeparator) + filename)
	//	if err == nil {
	//		return fileInfo.Size() == 0, nil
	//	} else {
	//		return false, err
	//	}
	//}
	//for i := 0; i < len(queue.moved); i++ {
	//	moved := queue.moved[i]
	//	empty, err := fileEmpty(moved.to)
	//	if err != nil { return err }
	//	if empty {
	//		queue.removeMoved(i)
	//		i--
	//		if err2 := queue.Add(Updated{filename: moved.to}); err2 != nil { return err2 }
	//		if !queue.HasModifications(moved.from) {
	//			if err2 := queue.Add(Deleted{filename: moved.from}); err2 != nil { return err2 }
	//		}
	//	}
	//}

	return nil
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

type UploadingModificationsQueue struct {
	updated []Updated
	deleted []Deleted
	moved   []Moved
}
func (queue UploadingModificationsQueue) Equals(other UploadingModificationsQueue) bool {
	if
		len(queue.updated) != len(other.updated) ||
		len(queue.deleted) != len(other.deleted) ||
		len(queue.moved) != len(other.moved,
	) {
		return false
	}

	for i, updated := range queue.updated {
		if updated != other.updated[i] { return false }
	}
	for i, deleted := range queue.deleted {
		if deleted != other.deleted[i] { return false }
	}
	for i, moved := range queue.moved {
		if moved != other.moved[i] { return false }
	}

	return true
}
