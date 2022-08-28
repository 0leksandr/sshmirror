package main

import (
	"sync"
)

type Modification interface {
	Join(queue *ModificationsQueue)
	AffectedPaths() []Path
}
// problem with created+updated: impossible to determine which one of the two is a moved file. I.e. a file was moved
// to a new location. Is it created or updated?
// TODO: return `Created`, and improve created + instantly deleted cases

// why refused (previously good and working) idea of stacking and optimizing moved and deletes:
// when started supporting directories, it became obvious, that there will always be conflicting cases:
// if we first move, then delete (which was the case), then deleting parent directory, and then moving into child
// requires changing order of operations

type Updated struct { // any file, that must be uploaded // MAYBE: Written
	path Path // MAYBE: Filename
}
type Deleted struct { // existing file, that was deleted
	path Path
}
type Moved struct { // existing file, that was moved to a new location
	from Path
	to   Path
}
func (updated Updated) Join(queue *ModificationsQueue) {
	addToQueue := true
	for _, previouslyUpdated := range queue.updated {
		if previouslyUpdated.path.Equals(updated.path) {
			addToQueue = false
			break
		}
	}
	if addToQueue { queue.updated = append(queue.updated, updated) }
}
func (deleted Deleted) Join(queue *ModificationsQueue) {
	for i, updated := range queue.updated {
		if deleted.path.IsParentOf(updated.path) {
			queue.removeUpdated(i) // PRIORITY: `i--` or something else appropriate. To this, and ALL other relevant places
		}
	}
	queue.inPlace = append(queue.inPlace, deleted)
}
func (moved Moved) Join(queue *ModificationsQueue) {
	if moved.from.Equals(moved.to) { return }

	for i, updated := range queue.updated {
		if moved.to.IsParentOf(updated.path) {
			queue.removeUpdated(i)
		}
	}
	for i, updated := range queue.updated {
		if moved.from.IsParentOf(updated.path) {
			Must(queue.updated[i].path.Move(moved.from, moved.to))
		}
	}

	queue.inPlace = append(queue.inPlace, moved)
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

type InPlaceModification interface { // MAYBE: rename
	Command(commander RemoteCommander) string
	OldFilename() Filename
	AffectedFiles() []Filename
	Equals(modification InPlaceModification) bool
}
func (deleted Deleted) Command(commander RemoteCommander) string {
	return commander.DeleteCommand(deleted.path)
}
func (deleted Deleted) OldFilename() Filename {
	return deleted.path.original
}
func (deleted Deleted) AffectedFiles() []Filename {
	return []Filename{deleted.path.original}
}
func (deleted Deleted) Equals(other InPlaceModification) bool {
	if otherDeleted, ok := other.(Deleted); ok {
		return deleted.path.Equals(otherDeleted.path)
	}
	return false
}
func (moved Moved) Command(commander RemoteCommander) string {
	return commander.MoveCommand(moved.from, moved.to)
}
func (moved Moved) OldFilename() Filename {
	return moved.from.original
}
func (moved Moved) AffectedFiles() []Filename {
	return []Filename{
		moved.from.original,
		moved.to.original,
	}
}
func (moved Moved) Equals(other InPlaceModification) bool {
	if otherMoved, ok := other.(Moved); ok {
		return moved.from.Equals(otherMoved.from) && moved.to.Equals(otherMoved.to)
	}
	return false
}

type ModificationsQueue struct {
	// MAYBE: map path: modification. For moved, key is moved.to
	updated []Updated
	inPlace []InPlaceModification
	mutex   sync.Mutex
}
func (queue *ModificationsQueue) AtomicAdd(modification Modification) {
	queue.mutex.Lock()
	queue.Add(modification)
	queue.mutex.Unlock()
}
func (queue *ModificationsQueue) Add(modification Modification) {
	modification.Join(queue)
}
func (queue *ModificationsQueue) IsEmpty() bool {
	queue.mutex.Lock()
	defer queue.mutex.Unlock()

	return len(queue.updated) == 0 && len(queue.inPlace) == 0
}
func (queue *ModificationsQueue) Copy() *ModificationsQueue {
	updated := make([]Updated, len(queue.updated))
	inPlace := make([]InPlaceModification, len(queue.inPlace))
	copy(updated, queue.updated)
	copy(inPlace, queue.inPlace)

	return &ModificationsQueue{
		updated: updated,
		inPlace: inPlace,
	}
}
func (queue *ModificationsQueue) Equals(other *ModificationsQueue) bool { // MAYBE: remove
	if len(queue.updated) != len(other.updated) || len(queue.inPlace) != len(other.inPlace) {
		return false
	}

	for i, updated := range queue.updated {
		if !updated.path.Equals(other.updated[i].path) { return false }
	}
	for i, inPlace := range queue.inPlace {
		if !inPlace.Equals(other.inPlace[i]) { return false }
	}

	return true
}
func (queue *ModificationsQueue) FlushInPlace() []InPlaceModification {
	queue.mutex.Lock()
	defer queue.mutex.Unlock()

	inPlace := make([]InPlaceModification, 0, len(queue.inPlace))
	copy(inPlace, queue.inPlace)
	queue.inPlace = []InPlaceModification{}
	return inPlace
}
func (queue *ModificationsQueue) removeUpdated(i int) {
	last := len(queue.updated) - 1
	if i != last { queue.updated[i] = queue.updated[last] }
	queue.updated = queue.updated[:last]
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
func (queue *TransactionalQueue) AtomicAdd(modification Modification) {
	queue.mutex.Lock()
	for _, _queue := range queue.queues() { _queue.AtomicAdd(modification) }
	queue.mutex.Unlock()
}
func (queue *TransactionalQueue) mustNotHaveStarted() {
	if queue.backup != nil { panic("transaction is active") }
}
func (queue *TransactionalQueue) queues() []*ModificationsQueue {
	queues := []*ModificationsQueue{&queue.ModificationsQueue}
	if queue.backup != nil { queues = append(queues, queue.backup) }
	return queues
}
