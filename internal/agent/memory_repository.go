package agent

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"sort"
	"sync"
)

type MemoryRepository struct {
	mu              sync.RWMutex
	records         map[SessionKey]Record
	events          map[SessionKey][]StoredEvent
	commands        map[SessionKey]map[string]storedCommand
	outbox          map[SessionKey][]StoredOutboxMessage
	retainedAfter   map[SessionKey]Cursor
	replayRetention Cursor
}

type storedCommand struct {
	digest  string
	version uint64
	cursor  Cursor
}

func NewMemoryRepository() *MemoryRepository {
	return newMemoryRepository(MaxRetainedReplayRecordsPerSession)
}

func newMemoryRepository(replayRetention Cursor) *MemoryRepository {
	return &MemoryRepository{
		records:         make(map[SessionKey]Record),
		events:          make(map[SessionKey][]StoredEvent),
		commands:        make(map[SessionKey]map[string]storedCommand),
		outbox:          make(map[SessionKey][]StoredOutboxMessage),
		retainedAfter:   make(map[SessionKey]Cursor),
		replayRetention: replayRetention,
	}
}

func (r *MemoryRepository) Load(ctx context.Context, key SessionKey) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	if err := key.Validate(); err != nil {
		return Record{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	record, ok := r.records[key]
	if !ok {
		return Record{}, fmt.Errorf("%w: session %s/%s", ErrNotFound, key.TenantID, key.SessionID)
	}
	return cloneRecord(record), nil
}

func (r *MemoryRepository) Commit(ctx context.Context, commit Commit) (CommitResult, error) {
	if err := ctx.Err(); err != nil {
		return CommitResult{}, err
	}
	if err := validateCommitIdentity(commit); err != nil {
		return CommitResult{}, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return CommitResult{}, err
	}

	commands := r.commands[commit.Key]
	if previous, ok := commands[commit.CommandID]; ok {
		if previous.digest != commit.CommandDigest {
			return CommitResult{}, fmt.Errorf("%w: command id %q was already used", ErrCommandConflict, commit.CommandID)
		}
		record, exists := r.records[commit.Key]
		if !exists {
			return CommitResult{}, fmt.Errorf("%w: command %q has no session projection", ErrCorruptState, commit.CommandID)
		}
		return CommitResult{
			Record:         cloneRecord(record),
			CommandVersion: previous.version,
			CommandCursor:  previous.cursor,
			Duplicate:      true,
		}, nil
	}
	if err := validateCommitBody(commit); err != nil {
		return CommitResult{}, err
	}

	current, exists := r.records[commit.Key]
	currentVersion := uint64(0)
	currentCursor := Cursor(0)
	if exists {
		currentVersion = current.Version
		currentCursor = current.Cursor
	}
	if exists && current.State.Status == SessionClosed {
		return CommitResult{}, fmt.Errorf("%w: session %s/%s", ErrSessionClosed, commit.Key.TenantID, commit.Key.SessionID)
	}
	if currentVersion != commit.ExpectedVersion {
		return CommitResult{}, fmt.Errorf("%w: expected %d, current %d", ErrVersionConflict, commit.ExpectedVersion, currentVersion)
	}
	if commit.Projection.TenantID != commit.Key.TenantID || commit.Projection.SessionID != commit.Key.SessionID {
		return CommitResult{}, fmt.Errorf("%w: projection identity does not match key", ErrInvalidArgument)
	}
	if commit.Projection.Version != currentVersion+uint64(len(commit.Events)) {
		return CommitResult{}, fmt.Errorf("%w: projection version %d does not match event count", ErrInvalidArgument, commit.Projection.Version)
	}
	if err := Validate(commit.Projection); err != nil {
		return CommitResult{}, fmt.Errorf("%w: projection: %v", ErrCorruptState, err)
	}
	base := NewState()
	if exists {
		base = current.State
	}
	replayed, err := ApplyAll(base, commit.Events)
	if err != nil || !reflect.DeepEqual(replayed, commit.Projection) {
		return CommitResult{}, fmt.Errorf("%w: projection does not match event replay", ErrCorruptState)
	}

	storedEvents := make([]StoredEvent, 0, len(commit.Events))
	cursor := currentCursor
	version := currentVersion
	for _, event := range commit.Events {
		cursor++
		version++
		storedEvents = append(storedEvents, StoredEvent{ID: eventID(commit.Key, cursor), Cursor: cursor, Version: version, Event: cloneEnvelope(event)})
	}
	storedOutbox := make([]StoredOutboxMessage, 0, len(commit.Outbox))
	for _, message := range commit.Outbox {
		cursor++
		storedOutbox = append(storedOutbox, StoredOutboxMessage{Cursor: cursor, Message: cloneOutbox(message)})
	}
	record := Record{Key: commit.Key, Version: commit.Projection.Version, Cursor: cursor, State: cloneState(commit.Projection)}
	result := CommitResult{
		Record:         record,
		CommandVersion: record.Version,
		CommandCursor:  record.Cursor,
		Outbox:         storedOutbox,
	}

	if commands == nil {
		commands = make(map[string]storedCommand)
		r.commands[commit.Key] = commands
	}
	r.records[commit.Key] = cloneRecord(record)
	r.events[commit.Key] = append(r.events[commit.Key], storedEvents...)
	r.outbox[commit.Key] = append(r.outbox[commit.Key], cloneStoredOutbox(storedOutbox)...)
	floor := retainedCursor(cursor, r.replayRetention)
	if floor > r.retainedAfter[commit.Key] {
		r.retainedAfter[commit.Key] = floor
		r.events[commit.Key] = retainStoredEventsAfter(r.events[commit.Key], floor)
		r.outbox[commit.Key] = retainStoredOutboxAfter(r.outbox[commit.Key], floor)
	}
	commands[commit.CommandID] = storedCommand{digest: commit.CommandDigest, version: result.CommandVersion, cursor: result.CommandCursor}
	return cloneCommitResult(result), nil
}

func (r *MemoryRepository) ReadEvents(ctx context.Context, key SessionKey, after Cursor, limit int) ([]StoredEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := key.Validate(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		return nil, fmt.Errorf("%w: limit must be positive", ErrInvalidArgument)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	record, ok := r.records[key]
	if !ok {
		return nil, fmt.Errorf("%w: session %s/%s", ErrNotFound, key.TenantID, key.SessionID)
	}
	if floor := r.retainedAfter[key]; after < floor {
		return nil, &CursorExpiredError{After: after, RetainedAfter: floor, SnapshotCursor: record.Cursor}
	}
	events := r.events[key]
	result := make([]StoredEvent, 0, min(limit, len(events)))
	for _, event := range events {
		if event.Cursor <= after {
			continue
		}
		result = append(result, cloneStoredEvent(event))
		if len(result) == limit {
			break
		}
	}
	return result, nil
}

func (r *MemoryRepository) ReadOutbox(ctx context.Context, key SessionKey, after Cursor, limit int) ([]StoredOutboxMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := key.Validate(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		return nil, fmt.Errorf("%w: limit must be positive", ErrInvalidArgument)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	record, ok := r.records[key]
	if !ok {
		return nil, fmt.Errorf("%w: session %s/%s", ErrNotFound, key.TenantID, key.SessionID)
	}
	if floor := r.retainedAfter[key]; after < floor {
		return nil, &CursorExpiredError{After: after, RetainedAfter: floor, SnapshotCursor: record.Cursor}
	}
	messages := r.outbox[key]
	result := make([]StoredOutboxMessage, 0, min(limit, len(messages)))
	for _, message := range messages {
		if message.Cursor <= after {
			continue
		}
		result = append(result, StoredOutboxMessage{Cursor: message.Cursor, Message: cloneOutbox(message.Message)})
		if len(result) == limit {
			break
		}
	}
	return result, nil
}

func (r *MemoryRepository) List(ctx context.Context, query SessionQuery) (SessionPage, error) {
	if err := ctx.Err(); err != nil {
		return SessionPage{}, err
	}
	if query.TenantID == "" {
		return SessionPage{}, fmt.Errorf("%w: tenant id is required", ErrInvalidArgument)
	}
	if query.Limit <= 0 {
		return SessionPage{}, fmt.Errorf("%w: limit must be positive", ErrInvalidArgument)
	}
	statuses := make(map[SessionStatus]bool, len(query.Statuses))
	for _, status := range query.Statuses {
		statuses[status] = true
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0)
	for key, record := range r.records {
		if key.TenantID != query.TenantID || key.SessionID <= query.AfterSessionID {
			continue
		}
		if !query.IncludeArchived && record.State.Archived {
			continue
		}
		if len(statuses) != 0 && !statuses[record.State.Status] {
			continue
		}
		ids = append(ids, key.SessionID)
	}
	sort.Strings(ids)
	page := SessionPage{Records: make([]Record, 0, min(query.Limit, len(ids)))}
	for _, id := range ids {
		if len(page.Records) == query.Limit {
			page.NextAfterSessionID = page.Records[len(page.Records)-1].Key.SessionID
			break
		}
		page.Records = append(page.Records, cloneRecord(r.records[SessionKey{TenantID: query.TenantID, SessionID: id}]))
	}
	return page, nil
}

func validateCommitIdentity(commit Commit) error {
	if err := commit.Key.Validate(); err != nil {
		return err
	}
	if commit.CommandID == "" || commit.CommandDigest == "" {
		return fmt.Errorf("%w: command id and digest are required", ErrInvalidArgument)
	}
	return nil
}

func validateCommitBody(commit Commit) error {
	if len(commit.Events) == 0 && len(commit.Outbox) != 0 {
		return fmt.Errorf("%w: no-op commit cannot append outbox messages", ErrInvalidArgument)
	}
	for _, event := range commit.Events {
		if event.Body == nil || event.CommandID != commit.CommandID || event.CommandDigest != commit.CommandDigest {
			return fmt.Errorf("%w: event command identity does not match commit", ErrInvalidArgument)
		}
	}
	seenOutbox := make(map[string]bool, len(commit.Outbox))
	for _, message := range commit.Outbox {
		if message.ID == "" || message.Topic == "" {
			return fmt.Errorf("%w: outbox id and topic are required", ErrInvalidArgument)
		}
		if seenOutbox[message.ID] {
			return fmt.Errorf("%w: duplicate outbox id %q", ErrInvalidArgument, message.ID)
		}
		seenOutbox[message.ID] = true
	}
	return nil
}

func eventID(key SessionKey, cursor Cursor) string {
	return fmt.Sprintf("%s/%s/event/%d", key.TenantID, key.SessionID, cursor)
}

func cloneRecord(record Record) Record {
	record.State = cloneState(record.State)
	return record
}

func cloneEnvelope(envelope Envelope) Envelope {
	switch event := envelope.Body.(type) {
	case InputSubmitted:
		event.Input = cloneInput(event.Input)
		event.Turn = cloneTurn(event.Turn)
		envelope.Body = event
	case AttemptAssignedEvent:
		event.Attempt = cloneTurn(Turn{Attempts: []Attempt{event.Attempt}}).Attempts[0]
		envelope.Body = event
	case AttemptPreparedContextSet:
		event.PreparedContext = bytes.Clone(event.PreparedContext)
		envelope.Body = event
	case AttemptOutputAppended:
		event.Output.Payload = bytes.Clone(event.Output.Payload)
		envelope.Body = event
	}
	return envelope
}

func cloneStoredEvent(event StoredEvent) StoredEvent {
	event.Event = cloneEnvelope(event.Event)
	return event
}

func cloneOutbox(message OutboxMessage) OutboxMessage {
	message.Payload = bytes.Clone(message.Payload)
	return message
}

func cloneStoredOutbox(messages []StoredOutboxMessage) []StoredOutboxMessage {
	clone := make([]StoredOutboxMessage, len(messages))
	for i, message := range messages {
		clone[i] = StoredOutboxMessage{Cursor: message.Cursor, Message: cloneOutbox(message.Message)}
	}
	return clone
}

func retainStoredEventsAfter(events []StoredEvent, floor Cursor) []StoredEvent {
	first := sort.Search(len(events), func(i int) bool { return events[i].Cursor > floor })
	return append([]StoredEvent(nil), events[first:]...)
}

func retainStoredOutboxAfter(messages []StoredOutboxMessage, floor Cursor) []StoredOutboxMessage {
	first := sort.Search(len(messages), func(i int) bool { return messages[i].Cursor > floor })
	return append([]StoredOutboxMessage(nil), messages[first:]...)
}

func cloneCommitResult(result CommitResult) CommitResult {
	result.Record = cloneRecord(result.Record)
	result.Outbox = cloneStoredOutbox(result.Outbox)
	return result
}
