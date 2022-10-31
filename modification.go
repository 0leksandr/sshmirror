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
	queue.fs.Update(updated.path)
}
func (deleted Deleted) Join(queue *ModificationsQueue) {
	queue.fs.Delete(deleted.path)
	queue.inPlace = append(queue.inPlace, deleted)
}
func (moved Moved) Join(queue *ModificationsQueue) {
	if moved.from.Equals(moved.to) {
		Updated{moved.from}.Join(queue)
		return
	}
	if moved.from.Relates(moved.to) { // MAYBE: test
		Deleted{moved.from}.Join(queue)
		Updated{moved.to}.Join(queue)
		return
	}

	queue.fs.Move(moved.from, moved.to)
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
	fs      *ModFS
	mutex   sync.Mutex
	inPlace []InPlaceModification
}
//goland:noinspection GoVetCopyLock
func (ModificationsQueue) New() *ModificationsQueue {
	return &ModificationsQueue{
		fs:      ModFS{}.New(),
		inPlace: make([]InPlaceModification, 0),
	}
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

	return len(queue.inPlace) == 0 && len(queue.fs.FetchUpdated(false)) == 0
}
func (queue *ModificationsQueue) Copy() *ModificationsQueue {
	return &ModificationsQueue{
		fs:      queue.fs.Copy(),
		inPlace: queue.copyInPlace(),
	}
}
func (queue *ModificationsQueue) GetInPlace(flush bool) []InPlaceModification {
	inPlace := queue.copyInPlace()
	if flush { queue.inPlace = make([]InPlaceModification, 0) }
	return inPlace
}
func (queue *ModificationsQueue) GetUpdated(flush bool) []Updated {
	return queue.fs.FetchUpdated(flush)
}
func (queue *ModificationsQueue) copyInPlace() []InPlaceModification {
	inPlace := make([]InPlaceModification, len(queue.inPlace))
	copy(inPlace, queue.inPlace)
	return inPlace
}

type TransactionalQueue struct { // TODO: rename
	ModificationsQueue
	backup *ModificationsQueue
	mutex  sync.Mutex
}
//goland:noinspection GoVetCopyLock
func (TransactionalQueue) New() *TransactionalQueue {
	return &TransactionalQueue{
		ModificationsQueue: *ModificationsQueue{}.New(),
	}
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
	defer queue.mutex.Unlock()

	queue.ModificationsQueue.AtomicAdd(modification)
	if queue.backup != nil { queue.backup.AtomicAdd(modification) }
}
func (queue *TransactionalQueue) GetInPlace(flush bool) []InPlaceModification {
	return queue.ModificationsQueue.GetInPlace(flush)
}
func (queue *TransactionalQueue) GetUpdated(flush bool) []Updated {
	return queue.ModificationsQueue.GetUpdated(flush)
}
func (queue *TransactionalQueue) mustNotHaveStarted() {
	if queue.backup != nil { panic("transaction is active") }
}
