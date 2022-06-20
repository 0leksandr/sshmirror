package main

import (
	"sync"
)

type Modification interface {
	Join(queue *ModificationsQueue)
	AffectedFiles() []Filename
}
// problem with created+updated: impossible to determine which one of the two is a moved file. I.e. a file was moved
// to a new location. Is it created or updated?
// TODO: return `Created`, and improve created + instantly deleted cases

// why refused (previously good and working) idea of stacking and optimizing moved and deletes:
// when started supporting directories, it became obvious, that there will always be conflicting cases:
// if we first move, then delete (which was the case), then deleting parent directory, and then moving into child
// requires changing order of operations

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
func (updated Updated) Join(queue *ModificationsQueue) {
	addToQueue := true
	for _, previouslyUpdated := range queue.updated {
		if previouslyUpdated.filename == updated.filename {
			addToQueue = false
		}
	}
	if addToQueue { queue.updated = append(queue.updated, updated) }
}
func (deleted Deleted) Join(queue *ModificationsQueue) {
	for i, updated := range queue.updated {
		if updated.filename == deleted.filename {
			queue.removeUpdated(i)
		}
	}
	queue.inPlace = append(queue.inPlace, deleted)
}
func (moved Moved) Join(queue *ModificationsQueue) {
	if moved.from == moved.to { return }

	addToQueue := true

	for i, updated := range queue.updated {
		if updated.filename == moved.to {
			queue.removeUpdated(i)
		}
	}
	for _, updated := range queue.updated {
		if updated.filename == moved.from { // PRIORITY: handle
			queue.Add(Deleted{moved.from})
			queue.Add(Updated{moved.to})
			addToQueue = false
		}
	}

	if addToQueue { queue.inPlace = append(queue.inPlace, moved) }
}
func (updated Updated) AffectedFiles() []Filename {
	return []Filename{updated.filename}
}
func (deleted Deleted) AffectedFiles() []Filename {
	return []Filename{deleted.filename}
}
func (moved Moved) AffectedFiles() []Filename {
	return []Filename{moved.from, moved.to}
}

type InPlaceModification interface { // MAYBE: rename
	Command(commander RemoteCommander) string
	OldFilename() Filename
	AffectedFiles() []Filename
}
func (deleted Deleted) Command(commander RemoteCommander) string {
	return commander.DeleteCommand(deleted.filename)
}
func (deleted Deleted) OldFilename() Filename {
	return deleted.filename
}
func (moved Moved) Command(commander RemoteCommander) string {
	return commander.MoveCommand(moved.from, moved.to)
}
func (moved Moved) OldFilename() Filename {
	return moved.from
}

type ModificationsQueue struct {
	// MAYBE: map filename: modification. For moved, key is moved.to
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
		if updated != other.updated[i] { return false }
	}
	for i, inPlace := range queue.inPlace {
		if inPlace != other.inPlace[i] { return false }
	}

	return true
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
