// (C) Copyright Confidential Containers Contributors
// SPDX-License-Identifier: Apache-2.0

package provider

// VolumeInfo holds provider-agnostic metadata about a block volume.
type VolumeInfo struct {
	VolumeID  string
	Path      string            // File path (Libvirt) or cloud volume ID (AWS EBS)
	SizeBytes int64
	Provider  string            // "libvirt", "aws"
	Metadata  map[string]string // Provider-specific data passed via mountInfo.json
}

// BlockVolumeProvider is the contract every cloud provider must implement.
// Scope: create and delete block volumes only (no resize, snapshot, clone).
type BlockVolumeProvider interface {
	// CreateVolume provisions a new block volume of the given size.
	// If the volume already exists, it returns the existing info (idempotent).
	CreateVolume(volumeID string, sizeBytes int64) (*VolumeInfo, error)

	// DeleteVolume removes a block volume.
	// Returns nil if the volume does not exist (idempotent).
	DeleteVolume(volumeID string) error

	// GetVolumeInfo returns metadata about an existing volume.
	GetVolumeInfo(volumeID string) (*VolumeInfo, error)

	// VolumeExists checks whether a volume with the given ID exists.
	VolumeExists(volumeID string) (bool, error)
}
