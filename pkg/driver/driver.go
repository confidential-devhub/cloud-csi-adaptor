// (C) Copyright Confidential Containers Contributors
// SPDX-License-Identifier: Apache-2.0

package driver

import (
	"fmt"
	"log"
	"net"
	"os"
	"strings"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc"
)

var drvLogger = log.New(log.Writer(), "[caa-csi/driver] ", log.LstdFlags|log.Lmsgprefix)

type Config struct {
	Endpoint      string
	DriverName    string
	VendorVersion string
	NodeID        string
}

type Driver struct {
	config Config
	server *grpc.Server
}

func NewDriver(cfg Config) (*Driver, error) {
	if cfg.DriverName == "" {
		return nil, fmt.Errorf("driver name is required")
	}
	if cfg.VendorVersion == "" {
		return nil, fmt.Errorf("driver version is required")
	}
	if cfg.NodeID == "" {
		return nil, fmt.Errorf("node ID is required")
	}

	return &Driver{config: cfg}, nil
}

func (d *Driver) Run() error {
	scheme, addr, err := parseEndpoint(d.config.Endpoint)
	if err != nil {
		return err
	}

	if scheme == "unix" {
		os.Remove(addr)
	}

	listener, err := net.Listen(scheme, addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s://%s: %w", scheme, addr, err)
	}

	d.server = grpc.NewServer()

	csi.RegisterIdentityServer(d.server, &identityServer{
		driverName:    d.config.DriverName,
		vendorVersion: d.config.VendorVersion,
	})
	csi.RegisterControllerServer(d.server, newControllerServer())
	csi.RegisterNodeServer(d.server, newNodeServer(d.config.NodeID))

	drvLogger.Printf("Listening on %s://%s (driver=%s, version=%s, node=%s)",
		scheme, addr, d.config.DriverName, d.config.VendorVersion, d.config.NodeID)

	return d.server.Serve(listener)
}

func (d *Driver) Stop() {
	if d.server != nil {
		d.server.GracefulStop()
	}
}

func parseEndpoint(endpoint string) (string, string, error) {
	if strings.HasPrefix(endpoint, "unix://") {
		return "unix", strings.TrimPrefix(endpoint, "unix://"), nil
	}
	if strings.HasPrefix(endpoint, "tcp://") {
		return "tcp", strings.TrimPrefix(endpoint, "tcp://"), nil
	}
	return "", "", fmt.Errorf("unsupported endpoint scheme: %s", endpoint)
}
