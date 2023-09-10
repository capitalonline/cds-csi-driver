package oss

import (
	"fmt"
	"github.com/capitalonline/cds-csi-driver/pkg/common"
	"github.com/kubernetes-csi/drivers/pkg/csi-common"
	"os"
	"os/signal"
	"syscall"
)

func NewDriver(driverName, nodeId, endpoint string) *OssDriver {
	d := &OssDriver{}
	d.endpoint = endpoint
	d.csiDriver = csicommon.NewCSIDriver(driverName, common.GetVersion().Version, nodeId)
	d.idServer = NewIdentityServer(d)
	d.nodeServer = NewNodeServer(d)
	return d
}

func (d *OssDriver) Run() {
	s := csicommon.NewNonBlockingGRPCServer()
	s.Start(d.endpoint, d.idServer, nil, d.nodeServer)
	//s.Wait()

	signals := make(chan os.Signal)

	// Notify signals to channel
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)

	// Block until a signal is received
	fmt.Println("Waiting for signal...")
	sig := <-signals
	fmt.Println("Received signal:", sig)

	// Exit program
	fmt.Println("Exiting program")
	os.Exit(0)
}
