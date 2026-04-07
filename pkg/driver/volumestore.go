// (C) Copyright Confidential Containers Contributors
// SPDX-License-Identifier: Apache-2.0

package driver

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const volumeStoreDir = "/var/lib/caa-csi-block/volumes"

type volumeRecord struct {
	VolumeID string            `json:"volumeID"`
	Provider string            `json:"provider"`
	Path     string            `json:"path"`
	Params   map[string]string `json:"params"`
}

type volumeStore struct {
	mu  sync.Mutex
	dir string
}

func newVolumeStore() *volumeStore {
	os.MkdirAll(volumeStoreDir, 0700)
	return &volumeStore{dir: volumeStoreDir}
}

func (vs *volumeStore) Save(rec *volumeRecord) error {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("failed to marshal volume record: %w", err)
	}
	return os.WriteFile(filepath.Join(vs.dir, rec.VolumeID+".json"), data, 0600)
}

func (vs *volumeStore) Load(volumeID string) (*volumeRecord, error) {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	data, err := os.ReadFile(filepath.Join(vs.dir, volumeID+".json"))
	if err != nil {
		return nil, err
	}
	var rec volumeRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("failed to unmarshal volume record: %w", err)
	}
	return &rec, nil
}

func (vs *volumeStore) Delete(volumeID string) {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	os.Remove(filepath.Join(vs.dir, volumeID+".json"))
}
