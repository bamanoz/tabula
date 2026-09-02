package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const leaseCommitRetries = 16

type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

type LeaseOptions struct {
	TTL               time.Duration
	HeartbeatInterval time.Duration
	Clock             Clock
}

type DriverIdentity struct {
	RuntimeID         string
	TenantID          string
	SessionID         string
	ComponentID       string
	AgentSpecRevision string
	DriverInstanceID  string
}

type LeaseGrant struct {
	Fence             Fence
	ExpiresAt         time.Time
	HeartbeatInterval time.Duration
	SessionVersion    uint64
}

type DriverLeaseService struct {
	repository        SessionRepository
	clock             Clock
	ttl               time.Duration
	heartbeatInterval time.Duration
}

func NewDriverLeaseService(repository SessionRepository, opts LeaseOptions) (*DriverLeaseService, error) {
	if repository == nil {
		return nil, fmt.Errorf("%w: session repository is required", ErrInvalidArgument)
	}
	if opts.TTL <= 0 || opts.HeartbeatInterval <= 0 || opts.HeartbeatInterval >= opts.TTL {
		return nil, fmt.Errorf("%w: lease ttl and shorter heartbeat interval are required", ErrInvalidArgument)
	}
	if opts.Clock == nil {
		opts.Clock = systemClock{}
	}
	return &DriverLeaseService{repository: repository, clock: opts.Clock, ttl: opts.TTL, heartbeatInterval: opts.HeartbeatInterval}, nil
}

func (s *DriverLeaseService) Register(ctx context.Context, identity DriverIdentity) (LeaseGrant, error) {
	if err := identity.Validate(); err != nil {
		return LeaseGrant{}, err
	}
	leaseID, err := randomLeaseID()
	if err != nil {
		return LeaseGrant{}, err
	}
	now := s.clock.Now()
	command := GrantDriverLease{
		Meta:              CommandMeta{ID: "driver-register:" + leaseID, Actor: ActorKernel},
		RuntimeID:         identity.RuntimeID,
		ComponentID:       identity.ComponentID,
		AgentSpecRevision: identity.AgentSpecRevision,
		DriverInstanceID:  identity.DriverInstanceID,
		LeaseID:           leaseID,
		Deadline:          now.Add(s.ttl),
	}
	record, err := s.commit(ctx, SessionKey{TenantID: identity.TenantID, SessionID: identity.SessionID}, command)
	if err != nil {
		return LeaseGrant{}, err
	}
	return LeaseGrant{Fence: record.State.Driver.Fence, ExpiresAt: record.State.Driver.Deadline, HeartbeatInterval: s.heartbeatInterval, SessionVersion: record.Version}, nil
}

func (s *DriverLeaseService) Ready(ctx context.Context, key SessionKey, runtimeID string, fence Fence) (Record, error) {
	if runtimeID == "" {
		return Record{}, fmt.Errorf("%w: runtime id is required", ErrInvalidArgument)
	}
	record, err := s.repository.Load(ctx, key)
	if err != nil {
		return Record{}, err
	}
	if record.State.Driver.RuntimeID != runtimeID {
		return Record{}, fmt.Errorf("%w: runtime identity does not own this lease", ErrPermissionDenied)
	}
	return s.commit(ctx, key, MarkDriverReady{Meta: CommandMeta{ID: fenceCommandID("driver-ready", fence), Actor: ActorDriver}, Fence: fence})
}

func (s *DriverLeaseService) Heartbeat(ctx context.Context, key SessionKey, runtimeID string, fence Fence, sequence uint64) (LeaseGrant, error) {
	now := s.clock.Now()
	command := RenewDriverLease{
		Meta:              CommandMeta{ID: fmt.Sprintf("driver-heartbeat:%s:%d:%d", fence.LeaseID, sequence, now.UnixNano()), Actor: ActorDriver},
		Fence:             fence,
		RuntimeID:         runtimeID,
		HeartbeatSequence: sequence,
		Deadline:          now.Add(s.ttl),
	}
	record, err := s.commit(ctx, key, command)
	if err != nil {
		return LeaseGrant{}, err
	}
	return LeaseGrant{Fence: record.State.Driver.Fence, ExpiresAt: record.State.Driver.Deadline, HeartbeatInterval: s.heartbeatInterval, SessionVersion: record.Version}, nil
}

func (s *DriverLeaseService) Release(ctx context.Context, key SessionKey, runtimeID string, fence Fence) (Record, error) {
	return s.commit(ctx, key, ReleaseDriverLease{
		Meta:      CommandMeta{ID: fenceCommandID("driver-release", fence), Actor: ActorDriver},
		Fence:     fence,
		RuntimeID: runtimeID,
	})
}

func (s *DriverLeaseService) Disconnect(ctx context.Context, key SessionKey, runtimeID string, fence Fence) (Record, error) {
	record, err := s.repository.Load(ctx, key)
	if err != nil {
		return Record{}, err
	}
	if record.State.Driver.RuntimeID != runtimeID {
		return Record{}, fmt.Errorf("%w: runtime identity does not own this lease", ErrPermissionDenied)
	}
	return s.commit(ctx, key, MarkDriverSuspect{Meta: CommandMeta{ID: fenceCommandID("driver-disconnect", fence), Actor: ActorKernel}, Fence: fence})
}

// DisconnectRuntime marks every active lease owned by a detached runtime suspect.
func (s *DriverLeaseService) DisconnectRuntime(ctx context.Context, tenantIDs []string, runtimeID string) error {
	if s == nil || s.repository == nil {
		return fmt.Errorf("driver lease service is not configured")
	}
	if runtimeID == "" {
		return fmt.Errorf("%w: runtime id is required", ErrInvalidArgument)
	}
	var disconnectErrors []error
	for _, tenantID := range tenantIDs {
		after := ""
		for {
			page, err := s.repository.List(ctx, SessionQuery{
				TenantID: tenantID, Statuses: []SessionStatus{SessionOpen},
				IncludeArchived: false, AfterSessionID: after, Limit: 100,
			})
			if err != nil {
				disconnectErrors = append(disconnectErrors, fmt.Errorf("list sessions for tenant %s: %w", tenantID, err))
				break
			}
			for _, record := range page.Records {
				if record.State.Driver.Status == DriverAbsent || record.State.Driver.RuntimeID != runtimeID {
					continue
				}
				if _, err := s.Disconnect(ctx, record.Key, runtimeID, record.State.Driver.Fence); err != nil {
					if errors.Is(err, ErrStaleDriver) || errors.Is(err, ErrPermissionDenied) {
						continue
					}
					disconnectErrors = append(disconnectErrors, fmt.Errorf("disconnect driver %s/%s: %w", record.Key.TenantID, record.Key.SessionID, err))
				}
			}
			if page.NextAfterSessionID == "" {
				break
			}
			after = page.NextAfterSessionID
		}
	}
	return errors.Join(disconnectErrors...)
}

func (s *DriverLeaseService) Expire(ctx context.Context, key SessionKey) (Record, error) {
	record, err := s.repository.Load(ctx, key)
	if err != nil {
		return Record{}, err
	}
	if record.State.Driver.Status == DriverAbsent {
		return record, nil
	}
	command := ExpireDriverLease{
		Meta: CommandMeta{ID: fmt.Sprintf("driver-expire:%s:%d", record.State.Driver.Fence.LeaseID, record.State.Driver.Deadline.UnixNano()), Actor: ActorKernel},
		Now:  s.clock.Now(),
	}
	return s.commit(ctx, key, command)
}

func (s *DriverLeaseService) commit(ctx context.Context, key SessionKey, command Command) (Record, error) {
	digest, err := DigestCommand(command)
	if err != nil {
		return Record{}, err
	}
	for range leaseCommitRetries {
		record, err := s.repository.Load(ctx, key)
		if err != nil {
			return Record{}, err
		}
		events, err := Decide(record.State, command)
		if err != nil {
			return Record{}, err
		}
		projection, err := ApplyAll(record.State, events)
		if err != nil {
			return Record{}, err
		}
		result, err := s.repository.Commit(ctx, Commit{
			Key:             key,
			CommandID:       command.Metadata().ID,
			CommandDigest:   digest,
			ExpectedVersion: record.Version,
			Events:          events,
			Projection:      projection,
			Outbox:          leaseOutbox(key, projection, events),
		})
		if errors.Is(err, ErrVersionConflict) {
			continue
		}
		if err != nil {
			return Record{}, err
		}
		return result.Record, nil
	}
	return Record{}, fmt.Errorf("commit driver lease after %d conflicts: %w", leaseCommitRetries, ErrVersionConflict)
}

func leaseOutbox(key SessionKey, state State, events []Envelope) []OutboxMessage {
	for _, envelope := range events {
		switch envelope.Body.(type) {
		case DriverBecameSuspect, DriverLeaseReleased, DriverLeaseExpired:
			payload, err := json.Marshal(struct {
				Key    SessionKey `json:"key"`
				Driver Driver     `json:"driver"`
				Queue  []string   `json:"queue"`
			}{Key: key, Driver: state.Driver, Queue: state.Queue})
			if err != nil {
				return nil
			}
			return []OutboxMessage{{ID: envelope.CommandID + ":driver-state", Topic: "driver.state_changed", Payload: payload}}
		}
	}
	return nil
}

func (i DriverIdentity) Validate() error {
	if i.RuntimeID == "" || i.TenantID == "" || i.SessionID == "" || i.ComponentID == "" || i.AgentSpecRevision == "" || i.DriverInstanceID == "" {
		return fmt.Errorf("%w: complete authenticated driver identity is required", ErrInvalidArgument)
	}
	return nil
}

func fenceCommandID(prefix string, fence Fence) string {
	return fmt.Sprintf("%s:%s:%d", prefix, fence.LeaseID, fence.Generation)
}

func randomLeaseID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate lease id: %w", err)
	}
	return "lease_" + hex.EncodeToString(raw[:]), nil
}
