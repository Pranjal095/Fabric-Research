/*
Copyright IBM Corp. 2016 All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package sharding

import (
	"encoding/json"
)

// PrepareRequestProto represents a serialized prepare request
type PrepareRequestProto struct {
	TxID      string
	ShardID   string
	ReadSet   map[string][]byte
	WriteSet  map[string][]byte
	Timestamp int64
}

// PrepareRequestBatch represents a batch of prepare requests
type PrepareRequestBatch struct {
	Requests []*PrepareRequestProto
}

// AbortEntry represents a transaction abort entry
type AbortEntry struct {
	TxID      string
	Timestamp int64
}

// Marshal serializes the batch to JSON
func (b *PrepareRequestBatch) Marshal() ([]byte, error) {
	return json.Marshal(b)
}

// Unmarshal deserializes the batch from JSON
func (b *PrepareRequestBatch) Unmarshal(data []byte) error {
	return json.Unmarshal(data, b)
}

// Marshal serializes the abort entry to JSON
func (a *AbortEntry) Marshal() ([]byte, error) {
	return json.Marshal(a)
}

// Unmarshal deserializes the abort entry from JSON
func (a *AbortEntry) Unmarshal(data []byte) error {
	return json.Unmarshal(data, a)
}
