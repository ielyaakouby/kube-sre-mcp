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

package kube

import (
	"errors"
	"fmt"
	"net"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

type Kind string

const (
	ErrNotFound     Kind = "NOT_FOUND"
	ErrForbidden    Kind = "FORBIDDEN"
	ErrUnauthorized Kind = "UNAUTHORIZED"
	ErrTimeout      Kind = "TIMEOUT"
	ErrConnection   Kind = "CONNECTION_ERROR"
	ErrAPI          Kind = "API_ERROR"
	ErrAmbiguous    Kind = "AMBIGUOUS"
	ErrInvalid      Kind = "INVALID"
	ErrCanceled     Kind = "CANCELED"
)

type Error struct {
	Kind      Kind
	Message   string
	Resource  string
	Namespace string
	Cause     error
}

func (e *Error) Error() string {
	if e.Resource != "" {
		return fmt.Sprintf("%s: %s (%s)", e.Kind, e.Message, e.Resource)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

func (e *Error) Unwrap() error { return e.Cause }

func Classify(err error) *Error {
	if err == nil {
		return nil
	}
	var ke *Error
	if errors.As(err, &ke) {
		return ke
	}
	if apierrors.IsNotFound(err) {
		return &Error{Kind: ErrNotFound, Message: "resource not found", Cause: err}
	}
	if apierrors.IsForbidden(err) {
		return &Error{Kind: ErrForbidden, Message: "permission denied", Cause: err}
	}
	if apierrors.IsUnauthorized(err) {
		return &Error{Kind: ErrUnauthorized, Message: "unauthorized", Cause: err}
	}
	if apierrors.IsTimeout(err) || strings.Contains(strings.ToLower(err.Error()), "deadline exceeded") {
		return &Error{Kind: ErrTimeout, Message: "kubernetes API timeout", Cause: err}
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return &Error{Kind: ErrConnection, Message: "connection error talking to the API server", Cause: err}
	}
	return &Error{Kind: ErrAPI, Message: err.Error(), Cause: err}
}

func IsForbidden(err error) bool {
	e := Classify(err)
	return e != nil && e.Kind == ErrForbidden
}

func IsNotFound(err error) bool {
	e := Classify(err)
	return e != nil && e.Kind == ErrNotFound
}
