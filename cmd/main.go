package main

import (
	"flag"
	"fmt"
	"github.com/capitalonline/cds-csi-driver/pkg/common"
	"github.com/capitalonline/cds-csi-driver/pkg/driver/ccsdisk"
	"github.com/capitalonline/cds-csi-driver/pkg/driver/disk"
	"github.com/capitalonline/cds-csi-driver/pkg/driver/ebs_disk"
	"github.com/capitalonline/cds-csi-driver/pkg/driver/eks_block"
	"github.com/capitalonline/cds-csi-driver/pkg/driver/nas"
	"github.com/capitalonline/cds-csi-driver/pkg/driver/oss"
	"github.com/capitalonline/cds-csi-driver/pkg/driver/utils"
	log "github.com/sirupsen/logrus"
	"os"
	"path"
)

const (
	DriverNasTypeName     = "nas.csi.cds.net"
	DriverOssTypeName     = "oss.csi.cds.net"
	DriverDiskTypeName    = "disk.csi.cds.net"
	DriverCCSDiskTypeName = "ccs-disk.csi.cds.net"
	DriverEbsDiskTypeName = "ebs-disk.csi.cds.net"
	uuidPath              = "/sys/devices/virtual/dmi/id/product_uuid"
	DriverEksDiskTypeName = "eks-disk.csi.cds.net"
)

var (
	endpointFlag = flag.String("endpoint", "unix://tmp/csi.sock", "CSI endpoint")
	driverFlag   = flag.String("driver", "", "CSI Driver")
	nodeIDFlag   = flag.String("nodeid", "", "node id")
	rootDirFlag  = flag.String("rootdir", "/var/lib/kubelet", "Kubernetes root directory")
	debugFlag    = flag.Bool("debug", false, "debug")
)

func init() {
	flag.Set("logtostderr", "true")
	flag.Parse()
}

func main() {
	endpoint := *endpointFlag
	driverName := *driverFlag
	nodeID := *nodeIDFlag
	rootDir := *rootDirFlag
	debug := *debugFlag

	if driverName == "" {
		log.Fatal("driver must be provided")
	}

	// set log config
	logType := os.Getenv("LOG_TYPE")
	if debug {
		logType = "stdout"
	}
	common.SetLogAttribute(logType, driverName)

	log.Infof("CSI Driver Version: %+v", common.GetVersion())
	if nodeID == "" {
		if driverName == DriverCCSDiskTypeName {
			// fetch vm uuid
			uuid, err := os.ReadFile(uuidPath)
			if err != nil {
				log.Fatalf("failed to read product_uuid file: %+v", err)
			}

			nodeID = string(uuid)
		} else {
			nodeID = utils.GetNodeMetadata().GetNodeID()
		}
	}

	log.Debugf("CSI Driver Name: %s", driverName)
	log.Debugf("CSI endpoint: %s", endpoint)
	log.Debugf("CSI node ID: %s", nodeID)

	if err := os.MkdirAll(path.Join(rootDir, "plugins", driverName, "controller"), os.FileMode(0755)); err != nil {
		log.Errorf("failed to create persistent storage for controller: %v", err)
		utils.SentrySendError(fmt.Errorf("failed to create persistent storage for controller: %v", err))
		os.Exit(1)
	}
	if err := os.MkdirAll(path.Join(rootDir, "plugins", driverName, "node"), os.FileMode(0755)); err != nil {
		log.Errorf("failed to create persistent storage for node: %v", err)
		utils.SentrySendError(fmt.Errorf("failed to create persistent storage for node: %v", err))
		os.Exit(1)
	}

	switch driverName {
	case DriverNasTypeName:
		nasDriver := nas.NewDriver(DriverNasTypeName, nodeID, endpoint)
		nasDriver.Run()
	case DriverOssTypeName:
		ossDriver := oss.NewDriver(DriverOssTypeName, nodeID, endpoint)
		ossDriver.Run()
	case DriverDiskTypeName:
		diskDriver := disk.NewDriver(DriverDiskTypeName, nodeID, endpoint)
		diskDriver.Run()
	case DriverCCSDiskTypeName:
		diskDriver := ccsdisk.NewDriver(DriverCCSDiskTypeName, nodeID, endpoint)
		diskDriver.Run()
	case DriverEbsDiskTypeName:
		diskDriver := ebs_disk.NewDriver(DriverEbsDiskTypeName, nodeID, endpoint)
		diskDriver.Run()
	case DriverEksDiskTypeName:
		diskDriver := eks_block.NewDriver(DriverEksDiskTypeName, nodeID, endpoint)
		diskDriver.Run()
	default:
		log.Fatalf("unsupported driver type: %s", driverName)
	}
}
