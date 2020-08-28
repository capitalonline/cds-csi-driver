package utils

import (
	"encoding/json"
	"fmt"
	"github.com/getsentry/sentry-go"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	NodeMetaDataFile = "/host/etc/cds/node-meta"
)

type NodeMeta struct {
	NodeID string `json:"node_id"`
}

// GetNodeId reads node metadata from file
func GetNodeMetadata() *NodeMeta {
	return func(f string) *NodeMeta {
		b, err := ioutil.ReadFile(f)
		if err != nil {
			log.Fatalf("cannot find metadata file %s: %s", f, err.Error())
		}
		var nodeMeta NodeMeta
		if err := json.Unmarshal(b, &nodeMeta); err != nil {
			log.Fatalf("failed to parse metadata file %s: %s", f, err.Error())
		}
		return &nodeMeta
	}(NodeMetaDataFile)
}

func (n *NodeMeta) GetNodeID() string {
	return n.NodeID
}

// Mounted checks whether a volume is mounted
func Mounted(mountPath string) bool {
	cmd := fmt.Sprintf("mount | grep %s | grep -v grep | wc -l", mountPath)
	out, err := RunCommand(cmd)
	if err != nil {
		log.Infof("check whether mounted exec error: %s, %s", cmd, err.Error())
		return false
	}
	if strings.TrimSpace(out) == "0" {
		return false
	}
	return true
}

// Unmount tries to unmount a device from the node
func Unmount(mountPath string) error {
	cmd := fmt.Sprintf("umount %s", mountPath)
	_, err := RunCommand(cmd)
	return err
}

// RunCommand runs a given shell command
func RunCommand(cmd string) (string, error) {
	out, err := exec.Command("sh", "-c", cmd).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("Failed to run cmd: " + cmd + ", with out: " + string(out) + ", with error: " + err.Error())
	}
	return string(out), nil
}

// CreateDir create the target directory with error handling
func CreateDir(target string, mode int) error {
	fi, err := os.Lstat(target)

	if os.IsNotExist(err) {
		if err := os.MkdirAll(target, os.FileMode(mode)); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	if fi != nil && !fi.IsDir() {
		return fmt.Errorf("%s already exist but it's not a directory", target)
	}
	return nil
}

// FileExisted checks if a file  or directory exists
func FileExisted(filename string) bool {
	_, err := os.Stat(filename)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	} else {
		return true
	}
}

// IsDir checks if the target path is directory
func IsDir(path string) bool {
	s, err := os.Stat(path)
	if err != nil {
		return false
	}
	return s.IsDir()
}

// WaitTimeOut waits for a mount of time before continues
func WaitTimeout(wg *sync.WaitGroup, timeout int) bool {
	c := make(chan struct{})
	go func() {
		defer close(c)
		wg.Wait()
	}()
	select {
	case <-c:
		return false
	case <-time.After(time.Duration(timeout) * time.Second):
		return true
	}
}

// ServerReachable tests whether a server is connection using TCP
func ServerReachable(host, port string, timeout time.Duration) bool {
	address := fmt.Sprintf("%s:%s", host, port)
	conn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		log.Errorf("server %s is not reachable", address)
		return false
	}
	defer conn.Close()
	return true
}

func SentrySendError(errorInfo error) {
	// will init by ENVIRONMENT named "SENTRY_DSN"
	err := sentry.Init(sentry.ClientOptions{
	})

	if err != nil {
		log.Fatalf("sentry.Init: %s", err)
	}

	// Flush buffered events before the program terminates.
	defer sentry.Flush(2 * time.Second)

	// 发送错误 sentry.CaptureException(exception error)
	sentry.CaptureException(errorInfo)
}
