// (C) Copyright Confidential Containers Contributors
// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v5"

	provider "github.com/confidential-containers/cloud-api-adaptor/src/caa-csi-block-driver/pkg/provider"
)

var logger = log.New(log.Writer(), "[caa-csi/azure] ", log.LstdFlags|log.Lmsgprefix)

func init() {
	provider.RegisterProvider("azure", func(params map[string]string) (provider.BlockVolumeProvider, error) {
		return NewAzureProvider(params)
	})
}

const (
	defaultDiskSKU = "StandardSSD_LRS"
	volumeTagKey   = "caa-csi-volume-id"
	pollInterval   = 5 * time.Second
	pollTimeout    = 2 * time.Minute
)

type Config struct {
	SubscriptionID string
	ResourceGroup  string
	Location       string
	DiskSKU        string
	TenantID       string
	ClientID       string
	ClientSecret   string
}

type AzureProvider struct {
	disksClient *armcompute.DisksClient
	config      Config
}

func NewAzureProvider(params map[string]string) (*AzureProvider, error) {
	subscriptionID := params["azureSubscriptionId"]
	if subscriptionID == "" {
		return nil, fmt.Errorf("azureSubscriptionId is required for azure provider")
	}

	resourceGroup := params["azureResourceGroup"]
	if resourceGroup == "" {
		return nil, fmt.Errorf("azureResourceGroup is required for azure provider")
	}

	location := params["azureLocation"]
	if location == "" {
		return nil, fmt.Errorf("azureLocation is required for azure provider")
	}

	diskSKU := params["azureDiskSKU"]
	if diskSKU == "" {
		diskSKU = defaultDiskSKU
	}

	cfg := Config{
		SubscriptionID: subscriptionID,
		ResourceGroup:  resourceGroup,
		Location:       location,
		DiskSKU:        diskSKU,
		TenantID:       params["azureTenantId"],
		ClientID:       params["azureClientId"],
		ClientSecret:   params["azureClientSecret"],
	}

	client, err := newDisksClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create Azure Disks client: %w", err)
	}

	return &AzureProvider{
		disksClient: client,
		config:      cfg,
	}, nil
}

func newDisksClient(cfg Config) (*armcompute.DisksClient, error) {
	var cred *azidentity.DefaultAzureCredential
	var err error

	if cfg.ClientID != "" && cfg.ClientSecret != "" && cfg.TenantID != "" {
		clientSecretCred, credErr := azidentity.NewClientSecretCredential(
			cfg.TenantID, cfg.ClientID, cfg.ClientSecret, nil)
		if credErr != nil {
			return nil, fmt.Errorf("failed to create client secret credential: %w", credErr)
		}
		return armcompute.NewDisksClient(cfg.SubscriptionID, clientSecretCred, nil)
	}

	cred, err = azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create default credential: %w", err)
	}

	return armcompute.NewDisksClient(cfg.SubscriptionID, cred, nil)
}

func diskName(volumeID string) string {
	return "csi-vol-" + volumeID
}

func (p *AzureProvider) CreateVolume(volumeID string, sizeBytes int64) (*provider.VolumeInfo, error) {
	ctx := context.TODO()

	exists, err := p.VolumeExists(volumeID)
	if err != nil {
		return nil, err
	}
	if exists {
		logger.Printf("Volume %s already exists, reusing", volumeID)
		return p.GetVolumeInfo(volumeID)
	}

	sizeGiB := int32(sizeBytes / (1024 * 1024 * 1024))
	if sizeGiB == 0 {
		sizeGiB = 1
	}

	name := diskName(volumeID)
	logger.Printf("Creating Azure Managed Disk %s (%d GiB, sku=%s, location=%s)",
		volumeID, sizeGiB, p.config.DiskSKU, p.config.Location)

	poller, err := p.disksClient.BeginCreateOrUpdate(ctx, p.config.ResourceGroup, name,
		armcompute.Disk{
			Location: to.Ptr(p.config.Location),
			SKU: &armcompute.DiskSKU{
				Name: to.Ptr(armcompute.DiskStorageAccountTypes(p.config.DiskSKU)),
			},
			Properties: &armcompute.DiskProperties{
				DiskSizeGB:       to.Ptr(sizeGiB),
				CreationData:     &armcompute.CreationData{CreateOption: to.Ptr(armcompute.DiskCreateOptionEmpty)},
				NetworkAccessPolicy: to.Ptr(armcompute.NetworkAccessPolicyAllowAll),
			},
			Tags: map[string]*string{
				volumeTagKey: to.Ptr(volumeID),
			},
		}, nil)
	if err != nil {
		return nil, fmt.Errorf("BeginCreateOrUpdate failed for %s: %w", volumeID, err)
	}

	disk, err := poller.PollUntilDone(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("disk creation polling failed for %s: %w", volumeID, err)
	}

	diskID := *disk.ID
	logger.Printf("Created Azure Managed Disk %s (disk-id=%s)", volumeID, diskID)

	return &provider.VolumeInfo{
		VolumeID:  volumeID,
		Path:      diskID,
		SizeBytes: sizeBytes,
		Provider:  "azure",
		Metadata: map[string]string{
			"cloud-volume-path": diskID,
			"cloud-provider":    "azure",
			"azure-disk-id":     diskID,
			"azure-disk-name":   name,
			"azure-location":    p.config.Location,
		},
	}, nil
}

func (p *AzureProvider) DeleteVolume(volumeID string) error {
	ctx := context.TODO()

	name := diskName(volumeID)

	exists, err := p.VolumeExists(volumeID)
	if err != nil || !exists {
		logger.Printf("Volume %s not found, nothing to delete", volumeID)
		return nil
	}

	logger.Printf("Deleting Azure Managed Disk %s (name=%s)", volumeID, name)

	poller, err := p.disksClient.BeginDelete(ctx, p.config.ResourceGroup, name, nil)
	if err != nil {
		return fmt.Errorf("BeginDelete failed for %s: %w", name, err)
	}

	if _, err := poller.PollUntilDone(ctx, nil); err != nil {
		return fmt.Errorf("disk deletion polling failed for %s: %w", name, err)
	}

	logger.Printf("Deleted Azure Managed Disk %s", name)
	return nil
}

func (p *AzureProvider) GetVolumeInfo(volumeID string) (*provider.VolumeInfo, error) {
	ctx := context.TODO()
	name := diskName(volumeID)

	disk, err := p.disksClient.Get(ctx, p.config.ResourceGroup, name, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get disk %s: %w", name, err)
	}

	var sizeBytes int64
	if disk.Properties != nil && disk.Properties.DiskSizeGB != nil {
		sizeBytes = int64(*disk.Properties.DiskSizeGB) * 1024 * 1024 * 1024
	}

	diskID := *disk.ID
	return &provider.VolumeInfo{
		VolumeID:  volumeID,
		Path:      diskID,
		SizeBytes: sizeBytes,
		Provider:  "azure",
		Metadata: map[string]string{
			"cloud-volume-path": diskID,
			"cloud-provider":    "azure",
			"azure-disk-id":     diskID,
			"azure-disk-name":   name,
			"azure-location":    p.config.Location,
		},
	}, nil
}

func (p *AzureProvider) VolumeExists(volumeID string) (bool, error) {
	ctx := context.TODO()
	name := diskName(volumeID)

	_, err := p.disksClient.Get(ctx, p.config.ResourceGroup, name, nil)
	if err != nil {
		return false, nil
	}
	return true, nil
}
