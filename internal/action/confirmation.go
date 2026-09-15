// Copyright 2026 The Kube SRE MCP Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrConfirmationMismatch = errors.New("confirmation_mismatch")
	ErrConfirmationExpired  = errors.New("confirmation_expired")
	ErrConfirmationReplay   = errors.New("confirmation_replay")
	ErrConfirmationMissing  = errors.New("confirmation_not_found")
)

type Pending struct {
	Action     string
	APIGroup   string
	Kind       string
	Name       string
	Namespace  string
	UID        string
	Context    string
	Replicas   *int32
	Grace      *int64
	Cordon     *bool
	HPAPresent bool
	Expires    time.Time
}

type Store struct {
	mu    sync.Mutex
	items map[string]Pending
	ttl   time.Duration
	now   func() time.Time
}

func NewStore(ttl time.Duration) *Store {
	if ttl == 0 {
		ttl = 2 * time.Minute
	}
	return &Store{items: map[string]Pending{}, ttl: ttl, now: time.Now}
}

func (s *Store) Put(p Pending) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcLocked()
	id, err := newID()
	if err != nil {
		return "", err
	}
	p.Expires = s.now().Add(s.ttl)
	s.items[id] = p
	return id, nil
}

func (s *Store) Take(id string, want Pending) (Pending, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcLocked()
	p, ok := s.items[id]
	if !ok {
		return Pending{}, ErrConfirmationMissing
	}
	if s.now().After(p.Expires) {
		delete(s.items, id)
		return Pending{}, ErrConfirmationExpired
	}
	if !bindMatch(p, want) {
		return Pending{}, ErrConfirmationMismatch
	}
	delete(s.items, id)
	return p, nil
}

func bindMatch(p, want Pending) bool {
	if p.Action != want.Action || p.Kind != want.Kind || p.Name != want.Name || p.Namespace != want.Namespace {
		return false
	}
	if p.APIGroup != want.APIGroup {
		return false
	}
	if p.Context != want.Context {
		return false
	}
	if p.UID != "" && want.UID != "" && p.UID != want.UID {
		return false
	}
	if !int32Eq(p.Replicas, want.Replicas) {
		return false
	}
	if !int64Eq(p.Grace, want.Grace) {
		return false
	}
	if !boolEq(p.Cordon, want.Cordon) {
		return false
	}
	return true
}

func int32Eq(a, b *int32) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func int64Eq(a, b *int64) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func boolEq(a, b *bool) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func (s *Store) gcLocked() {
	now := s.now()
	for k, v := range s.items {
		if now.After(v.Expires) {
			delete(s.items, k)
		}
	}
}

func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("confirmation token entropy: %w", err)
	}
	return hex.EncodeToString(b), nil
}
