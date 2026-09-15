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
	"kube-sre-mcp/internal/model"
	"kube-sre-mcp/internal/security"
)

func Record(v *model.Visibility, operation, resource, namespace string, err error) {
	if v == nil || err == nil {
		return
	}
	v.Complete = false
	v.Limited = true
	ce := Classify(err)
	fc := model.FailedCheck{
		Operation: operation,
		Resource:  resource,
		ErrorType: string(ce.Kind),
		Message:   security.Redact(ce.Error()),
	}
	v.FailedChecks = append(v.FailedChecks, fc)
	if ce.Kind == ErrForbidden || ce.Kind == ErrUnauthorized {
		v.MissingPermissions = append(v.MissingPermissions, model.Permission{
			Verb:      operation,
			Resource:  resource,
			Namespace: namespace,
			Message:   security.Redact(ce.Message),
		})
	}
}

func IsVisibilityFailure(err error) bool {
	if err == nil {
		return false
	}
	switch Classify(err).Kind {
	case ErrForbidden, ErrUnauthorized, ErrTimeout, ErrConnection, ErrAPI, ErrCanceled:
		return true
	default:
		return false
	}
}
