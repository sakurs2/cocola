package main

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type replaySource struct {
	ids []string
	err error
}

func (s replaySource) RevokedIDs(context.Context) ([]string, error) {
	return s.ids, s.err
}

type replayPublisher struct {
	ids    []string
	failAt int
}

func (p *replayPublisher) Revoke(_ context.Context, id string) error {
	if len(p.ids) == p.failAt {
		return errors.New("redis unavailable")
	}
	p.ids = append(p.ids, id)
	return nil
}

func TestReplayRevokedTokens(t *testing.T) {
	publisher := &replayPublisher{failAt: -1}
	count, err := replayRevokedTokens(
		context.Background(),
		replaySource{ids: []string{"jti-1", "jti-2"}},
		publisher,
	)
	if err != nil || count != 2 {
		t.Fatalf("replay = %d, %v", count, err)
	}
	if !reflect.DeepEqual(publisher.ids, []string{"jti-1", "jti-2"}) {
		t.Fatalf("published ids = %v", publisher.ids)
	}
}

func TestReplayRevokedTokensFailsClosed(t *testing.T) {
	publisher := &replayPublisher{failAt: 1}
	count, err := replayRevokedTokens(
		context.Background(),
		replaySource{ids: []string{"jti-1", "jti-2", "jti-3"}},
		publisher,
	)
	if err == nil || count != 1 {
		t.Fatalf("replay = %d, %v; want one success then failure", count, err)
	}
	if !reflect.DeepEqual(publisher.ids, []string{"jti-1"}) {
		t.Fatalf("publisher continued after failure: %v", publisher.ids)
	}
}

func TestReplayRevokedTokensPropagatesListFailure(t *testing.T) {
	count, err := replayRevokedTokens(
		context.Background(),
		replaySource{err: errors.New("postgres unavailable")},
		&replayPublisher{failAt: -1},
	)
	if err == nil || count != 0 {
		t.Fatalf("replay = %d, %v; want list failure", count, err)
	}
}
