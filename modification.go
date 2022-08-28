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
func (updated Updated) Join(queue *ModificationsQueue) error {
	queue.fs.Update(updated.filename)
	return nil
}
func (deleted Deleted) Join(queue *ModificationsQueue) error {
	queue.fs.Delete(Pathname{
		Filename: deleted.filename,
		IsDir:    false,
	})
	return nil
}
func (moved Moved) Join(queue *ModificationsQueue) error {
	queue.fs.Move(
		Pathname{
			Filename: moved.from,
			IsDir:    false,
		},
		moved.to,
	)
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
	fs    *ModFS
	mutex sync.Mutex
}
//goland:noinspection GoVetCopyLock
func (ModificationsQueue) New() *ModificationsQueue {
	return &ModificationsQueue{
		fs: ModFS{}.New(),
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
func (queue *ModificationsQueue) HasModifications(filename Filename) bool {
	return queue.fs.changes.Has(queue.fs.getFile(filename))
}
func (queue *ModificationsQueue) IsEmpty() bool {
	queue.mutex.Lock()
	defer queue.mutex.Unlock()

	return queue.fs.changes.Len() == 0
}
func (queue *ModificationsQueue) Copy() *ModificationsQueue {
	return &ModificationsQueue{
		fs: queue.fs.Copy(),
	}
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
