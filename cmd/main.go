package main

import (
	"flag"
	"os"
	"path"

	"github.com/capitalonline/cds-csi-driver/pkg/common"
	"github.com/capitalonline/cds-csi-driver/pkg/driver/nas"
	"github.com/capitalonline/cds-csi-driver/pkg/driver/utils"

	log "github.com/sirupsen/logrus"
)

const (
	// TypePluginNAS NAS type plugin
	DriverNasType     = "nas"
	DriverNasTypeName = "nas.csi.cds.net"
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

	if nodeID == "" {
		nodeID = utils.GetNodeId()
	}

	log.Infof("CSI Driver Name: %s, %s", driverName, endpoint)
	log.Infof("CSI Driver Version: %+v", common.GetVersion())

	if err := os.MkdirAll(path.Join(rootDir, "plugins", driverName, "controller"), os.FileMode(0755)); err != nil {
		log.Errorf("failed to create persistent storage for controller: %v", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(path.Join(rootDir, "plugins", driverName, "node"), os.FileMode(0755)); err != nil {
		log.Errorf("failed to create persistent storage for node: %v", err)
		os.Exit(1)
	}

	switch driverName {
	case DriverNasType:
		nasDriver := nas.NewDriver(DriverNasTypeName, nodeID, endpoint)
		nasDriver.Run()
	default:
		log.Fatal("unsupported driver type: %s", driverName)
	}

}
