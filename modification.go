package main

import (
	"errors"
	"sync"
)

type Modification interface {
	Join(queue *ModificationsQueue) error
	AffectedPaths() []Path
}
// problem with created+updated: impossible to determine which one of the two is a moved file. I.e. a file was moved
// to a new location. Is it created or updated?
// TODO: return `Created`, and improve created + instantly deleted cases

type Updated struct { // any file, that must be uploaded // MAYBE: Written
	path Path
}
type Deleted struct { // existing file, that was deleted
	path Path
}
type Moved struct { // existing file, that was moved to a new location
	from Path
	to   Path
}
func (updated Updated) Join(queue *ModificationsQueue) error {
	addToQueue := true
	for _, previouslyUpdated := range queue.updated {
		if previouslyUpdated.path.Equals(updated.path) {
			addToQueue = false
		} else if previouslyUpdated.path.Relates(updated.path) {
			inconsistentModifications()
		}

		//if previouslyUpdated.path.IsParentOf(updated.path) {
		//	addToQueue = false
		//} else if updated.path.IsParentOf(previouslyUpdated.path) {
		//	queue.removeUpdated(i) // MAYBE: just replace old one with a new one
		//}
	}
	for i, moved := range queue.moved {
		if moved.to.Equals(updated.path) {
			queue.removeMoved(i)
			if !queue.HasModifications(moved.from) {
				if err := queue.Add(Deleted{moved.from}); err != nil { return err }
			}
		} else if moved.to.IsParentOf(updated.path) {
			// ignore
		} else if updated.path.IsParentOf(moved.to) {
			inconsistentModifications()
		}
	}
	for i, deleted := range queue.deleted {
		if deleted.path.Equals(updated.path) {
			queue.removeDeleted(i)
		} else if deleted.path.IsParentOf(updated.path) {
			// ignore
		} else if updated.path.IsParentOf(deleted.path) {
			inconsistentModifications()
		}
	}
	if addToQueue { queue.updated = append(queue.updated, updated) }
	return nil
}
func (deleted Deleted) Join(queue *ModificationsQueue) error {
	for i, updated := range queue.updated {
		if deleted.path.IsParentOf(updated.path) {
			queue.removeUpdated(i) // PRIORITY: `i--` or something else appropriate. To this, and ALL other relevant places
		} else if updated.path.IsParentOf(deleted.path) {
			inconsistentModifications()
		}
	}
	for i, moved := range queue.moved {
		if deleted.path.IsParentOf(moved.to) {
			queue.removeMoved(i)
			if !queue.HasModifications(moved.from) {
				if err := queue.Add(Deleted{moved.from}); err != nil { return err }
			}
		} else if moved.to.IsParentOf(deleted.path) {
			// ignore
		}
	}
	for i, previouslyDeleted := range queue.deleted {
		if previouslyDeleted.path.IsParentOf(deleted.path) {
			return errors.New("trying to delete what is already deleted")
		} else if deleted.path.IsParentOf(previouslyDeleted.path) {
			queue.removeDeleted(i)
		}
	}
	queue.deleted = append(queue.deleted, deleted)
	return nil
}
func (moved Moved) Join(queue *ModificationsQueue) error {
	if moved.from.Relates(moved.to) { return errors.New("impossible move") } // TODO: try to reproduce

	addToQueue := true
	for i, deleted := range queue.deleted { // before the next block, because else will be overwritten
		if deleted.path.IsParentOf(moved.from) {
			return errors.New("trying to move that was deleted")
		} else if moved.from.IsParentOf(deleted.path) {
			// PRIORITY: shift deleted
		}

		if moved.to.IsParentOf(deleted.path) {
			queue.removeDeleted(i)
		} else if deleted.path.IsParentOf(moved.to) {
			// PRIORITY: reorder operations?
		}
	}

	for i, updated := range queue.updated {
		if moved.to.IsParentOf(updated.path) {
			queue.removeUpdated(i)
		} else if updated.path.IsParentOf(moved.to) {
			inconsistentModifications()
		}
	}
	for _, updated := range queue.updated {
		if updated.path.Equals(moved.from) {
			if err := queue.Add(Deleted{moved.from}); err != nil { return err }
			if err := queue.Add(Updated{moved.to}); err != nil { return err }
			addToQueue = false
		} else if updated.path.IsParentOf(moved.from) {
			inconsistentModifications()
		} else if moved.from.IsParentOf(updated.path) {
			// PRIORITY: shift updated
		}
	}

	for i, previouslyMoved := range queue.moved {
		if previouslyMoved.from.IsParentOf(moved.from) {
			return errors.New("moved from, then moved from")
		}

		if previouslyMoved.to.Equals(moved.from) {
			//triangularMove := false // TODO: uncomment and test modifying files within triangle
			//for i2, previouslyMoved2 := range queue.moved {
			//	if previouslyMoved2.from == moved.to {
			//		if i != i2 { triangularMove = true }
			//	}
			//}
			//if !triangularMove {
			//}
			if previouslyMoved.from.Equals(moved.to) {
				queue.removeMoved(i)
			} else {
				queue.moved[i].to = moved.to
			}
			if err := queue.Add(Deleted{moved.from}); err != nil { return err }
			addToQueue = false
		}
		if previouslyMoved.to.Equals(moved.to) {
			queue.removeMoved(i)
			if !queue.HasModifications(previouslyMoved.from) {
				if err := queue.Add(Deleted{previouslyMoved.from}); err != nil { return err }
			}
		}
	}
	if addToQueue { queue.moved = append(queue.moved, moved) }
	return nil
}
func (updated Updated) AffectedPaths() []Path {
	return []Path{updated.path}
}
func (deleted Deleted) AffectedPaths() []Path {
	return []Path{deleted.path}
}
func (moved Moved) AffectedPaths() []Path {
	return []Path{moved.from, moved.to}
}

type ModificationsQueue struct {
	// MAYBE: map path: modification. For moved, key is moved.to
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
func (queue *ModificationsQueue) HasModifications(path Path) bool {
	// PRIORITY: check tree all the way down
	for _, updated := range queue.updated {
		if updated.path.Relates(path) { return true }
	}
	for _, deleted := range queue.deleted {
		if deleted.path.Relates(path) { return true }
	}
	for _, moved := range queue.moved {
		if moved.to.Relates(path){ return true }
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
func (queue *ModificationsQueue) Optimize() error {
	queue.mutex.Lock()
	defer queue.mutex.Unlock()

	// check for circular move
	removedCircular := true // TODO: remove after triangular move is uncommented
	for removedCircular {
		var err error
		removedCircular, err = (func() (bool, error) {
			for i := 0; i < len(queue.moved); i++ {
				movedI := queue.moved[i]
				for j := i+1; j < len(queue.moved); j++ {
					movedJ := queue.moved[j]
					if movedI.from.Equals(movedJ.to) && movedI.to.Equals(movedJ.from) {
						queue.removeMoved(j)
						queue.removeMoved(i)
						// THINK: something smarter
						var err2 error
						for _, path := range [2]Path{movedI.from , movedJ.from} {
							if err3 := queue.Add(Updated{path}); err3 != nil { err2 = err3 }
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
	//		if err2 := queue.Add(Updated{path: moved.to}); err2 != nil { return err2 }
	//		if !queue.HasModifications(moved.from) {
	//			if err2 := queue.Add(Deleted{path: moved.from}); err2 != nil { return err2 }
	//		}
	//	}
	//}

	return nil
}
func (queue *ModificationsQueue) Equals(other *ModificationsQueue) bool { // MAYBE: remove
	if len(queue.updated) != len(other.updated) ||
		len(queue.deleted) != len(other.deleted) ||
		len(queue.moved) != len(other.moved,
	) {
		return false
	}

	for i, updated := range queue.updated {
		if !updated.path.Equals(other.updated[i].path) { return false }
	}
	for i, deleted := range queue.deleted {
		if !deleted.path.Equals(other.deleted[i].path) { return false }
	}
	for i, moved := range queue.moved {
		if !moved.from.Equals(other.moved[i].from) || !moved.to.Equals(other.moved[i].to) { return false }
	}

	return true
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

type TransactionalQueue struct { // TODO: rename
	ModificationsQueue
	backup *ModificationsQueue
	mutex  sync.Mutex
}
func (queue *TransactionalQueue) Begin() {
	queue.mutex.Lock()
	queue.mustNotHaveStarted()
	queue.backup = queue.ModificationsQueue.Copy()
	queue.mutex.Unlock()
}
func (queue *TransactionalQueue) Commit() {
	queue.mutex.Lock()
	queue.backup = nil
	queue.mutex.Unlock()
}
func (queue *TransactionalQueue) Rollback() {
	queue.mutex.Lock()
	queue.ModificationsQueue = *queue.backup.Copy() // THINK: is `Copy` necessary?
	queue.backup = nil
	queue.mutex.Unlock()
}
func (queue *TransactionalQueue) IsEmpty() bool {
	queue.mustNotHaveStarted()
	return queue.ModificationsQueue.IsEmpty()
}
func (queue *TransactionalQueue) AtomicAdd(modification Modification) error {
	queue.mutex.Lock()
	defer queue.mutex.Unlock()

	for _, _queue := range queue.queues() {
		if err := _queue.AtomicAdd(modification); err != nil { return err }
	}
	return nil
}
func (queue *TransactionalQueue) mustNotHaveStarted() {
	if queue.backup != nil { panic("transaction is active") }
}
func (queue *TransactionalQueue) queues() []*ModificationsQueue {
	queues := []*ModificationsQueue{&queue.ModificationsQueue}
	if queue.backup != nil { queues = append(queues, queue.backup) }
	return queues
}

func inconsistentModifications() { // THINK: resolve
	panic("inconsistent modifications")
}
