package main

import (
	"sync"
)

type Modification interface {
	Join(queue *ModificationsQueue) error
	AffectedFiles() []Filename
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
func (updated Updated) AffectedFiles() []Filename {
	return []Filename{updated.filename}
}
func (deleted Deleted) AffectedFiles() []Filename {
	return []Filename{deleted.filename}
}
func (moved Moved) AffectedFiles() []Filename {
	return []Filename{moved.from, moved.to}
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
func (queue *ModificationsQueue) AtomicAdd(modification Modification) error {
	queue.mutex.Lock()
	defer queue.mutex.Unlock()

	return queue.Add(modification)
}
func (queue *ModificationsQueue) Add(modification Modification) error {
	return modification.Join(queue)
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
