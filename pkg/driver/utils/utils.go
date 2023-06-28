package utils

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/getsentry/sentry-go"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"io/ioutil"
	"k8s.io/apimachinery/pkg/api/resource"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	NodeMetaDataFile = "/host/etc/cds/node-meta"
	CloudInitDevSize = 8 * 1024
)

// Metrics represents the used and available bytes of the Volume.
type Metrics struct {
	// The time at which these stats were updated.
	Time metav1.Time

	// Used represents the total bytes used by the Volume.
	// Note: For block devices this maybe more than the total size of the files.
	Used *resource.Quantity

	// Capacity represents the total capacity (bytes) of the volume's
	// underlying storage. For Volumes that share a filesystem with the host
	// (e.g. emptydir, hostpath) this is the size of the underlying storage,
	// and will not equal Used + Available as the fs is shared.
	Capacity *resource.Quantity

	// Available represents the storage space available (bytes) for the
	// Volume. For Volumes that share a filesystem with the host (e.g.
	// emptydir, hostpath), this is the available space on the underlying
	// storage, and is shared with host processes and other Volumes.
	Available *resource.Quantity

	// InodesUsed represents the total inodes used by the Volume.
	InodesUsed *resource.Quantity

	// Inodes represents the total number of inodes available in the volume.
	// For volumes that share a filesystem with the host (e.g. emptydir, hostpath),
	// this is the inodes available in the underlying storage,
	// and will not equal InodesUsed + InodesFree as the fs is shared.
	Inodes *resource.Quantity

	// InodesFree represent the inodes available for the volume.  For Volumes that share
	// a filesystem with the host (e.g. emptydir, hostpath), this is the free inodes
	// on the underlying storage, and is shared with host processes and other volumes
	InodesFree *resource.Quantity
}

type NodeMeta struct {
	NodeID string `json:"node_id"`
}

// GetNodeId reads node metadata from file
func GetNodeMetadata() *NodeMeta {
	return func(f string) *NodeMeta {
		b, err := ioutil.ReadFile(f)
		if err != nil {
			// 尝试从cloud-init盘读取数据
			id, err := ReadCloudInitInfo()
			log.Infof("successfully get instance id from cloud init info")
			if err != nil {
				log.Fatalf("cannot find metadata file %s: %s", f, err.Error())
			}
			return &NodeMeta{NodeID: id}
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

func ReadCloudInitInfo() (string, error) {

	output, err := exec.Command("sh", "-c", "cat /proc/partitions | grep 8192").CombinedOutput()
	fmt.Println(output, err)
	list := strings.Split(strings.TrimSpace(string(output)), " ")
	var devName = ""
	for _, item := range list {
		if !strings.HasPrefix(item, "sd") {
			continue
		}
		devName = item
	}
	if devName == "" {
		return "", errors.New("cannot find cloudinit device")
	}

	if err = os.Mkdir("/tmp/ins", 0777); err != nil && !os.IsExist(err) {
		msg := fmt.Sprintf("can not make dir err:%s", err.Error())
		log.Errorf(msg)
		return "", errors.New(msg)
	}
	cmd := exec.Command("mount", fmt.Sprintf("/dev/%s", devName), "/tmp/ins")
	_, err = cmd.CombinedOutput()
	if err != nil {
		return "", errors.New("cannot mount device")
	}
	file, err := os.ReadFile("/tmp/ins/meta-data")
	if err != nil {
		return "", errors.New(fmt.Sprintf("cannot open /tmp/ins/meta-data,err:%s", err.Error()))
	}

	list = strings.Split(string(file), "\n")
	var instanceId = ""
	for _, item := range list {
		if strings.Contains(item, "local-hostname") {
			item = strings.Trim(item, "local-hostname:")
			instanceId = strings.Trim(strings.TrimSpace(item), " ")
		}
	}
	_, _ = exec.Command("umount", "/tmp/ins").CombinedOutput()
	if len(instanceId) == 0 {
		return "", errors.New(fmt.Sprintf("invalid instance_id"))
	}
	return instanceId, nil

	//dir, err := os.Open("/dev")
	//if err != nil {
	//	log.Errorf("can not open dev")
	//	return "", err
	//}
	//defer dir.Close()
	//
	//// 读取目录中的文件信息
	//files, err := dir.Readdir(-1)
	//if err != nil {
	//
	//	return "", err
	//}
	//
	//var blocks = make([]os.FileInfo, 0)
	//
	//// 遍历文件信息
	//for _, file := range files {
	//	// 检查是否为块设备文件
	//	if (file.Mode()&os.ModeDevice) == 0 || (file.Mode()&os.ModeCharDevice) != 0 {
	//		continue
	//	}
	//	blocks = append(blocks, file)
	//	// 获取设备号
	//	//dev := file.Sys().(*syscall.Stat_t).Rdev
	//	//
	//	//// 输出设备路径和设备号
	//	//fmt.Printf("设备路径：%s\n", "/dev/"+file.Name())
	//	//fmt.Printf("设备号：%d:%d\n", uint32(dev/256), uint32(dev%256))
	//	//fmt.Println()
	//}
	//
	//for _, dev := range blocks {
	//	file, err := os.Open(fmt.Sprintf("/dev/%s", dev.Name()))
	//	defer file.Close()
	//	if err != nil {
	//		continue
	//	}
	//	fd := int(file.Fd())
	//	var stat syscall.Stat_t
	//	if err = syscall.Fstat(fd, &stat); err != nil {
	//		continue
	//	}
	//
	//	// 输出块设备大小（以字节为单位）
	//	blockSize := int64(stat.Blksize)
	//	diskSize := uint64(stat.Blocks) * uint64(blockSize)
	//	if diskSize == CloudInitDevSize {
	//		if err = os.Mkdir("/tmp/ins", 0777); err != nil && !os.IsExist(err) {
	//			msg := fmt.Sprintf("can not make dir err:%s", err.Error())
	//			log.Errorf(msg)
	//			panic(msg)
	//		}
	//		cmd := exec.Command("mount", fmt.Sprintf("/dev/%s", dev.Name()), "/tmp/ins")
	//		output, err := cmd.CombinedOutput()
	//		fmt.Println(string(output), "   ", err)
	//	}
	//}

	return "", errors.New("cat not found instance info")
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
	err := sentry.Init(sentry.ClientOptions{})

	if err != nil {
		log.Fatalf("sentry.Init: %s", err)
	}

	// Flush buffered events before the program terminates.
	defer sentry.Flush(2 * time.Second)

	// 发送错误 sentry.CaptureException(exception error)
	sentry.CaptureException(errorInfo)
}

// GetMetrics get path metric
func GetMetrics(path string) (*csi.NodeGetVolumeStatsResponse, error) {
	if path == "" {
		return nil, fmt.Errorf("getMetrics No path given")
	}
	available, capacity, usage, inodes, inodesFree, inodesUsed, err := FsInfo(path)
	if err != nil {
		return nil, err
	}

	metrics := &Metrics{Time: metav1.Now()}
	metrics.Available = resource.NewQuantity(available, resource.BinarySI)
	metrics.Capacity = resource.NewQuantity(capacity, resource.BinarySI)
	metrics.Used = resource.NewQuantity(usage, resource.BinarySI)
	metrics.Inodes = resource.NewQuantity(inodes, resource.BinarySI)
	metrics.InodesFree = resource.NewQuantity(inodesFree, resource.BinarySI)
	metrics.InodesUsed = resource.NewQuantity(inodesUsed, resource.BinarySI)

	metricAvailable, ok := (*(metrics.Available)).AsInt64()
	if !ok {
		log.Errorf("failed to fetch available bytes for target: %s", path)
		return nil, status.Error(codes.Unknown, "failed to fetch available bytes")
	}
	metricCapacity, ok := (*(metrics.Capacity)).AsInt64()
	if !ok {
		log.Errorf("failed to fetch capacity bytes for target: %s", path)
		return nil, status.Error(codes.Unknown, "failed to fetch capacity bytes")
	}
	metricUsed, ok := (*(metrics.Used)).AsInt64()
	if !ok {
		log.Errorf("failed to fetch used bytes for target %s", path)
		return nil, status.Error(codes.Unknown, "failed to fetch used bytes")
	}
	metricInodes, ok := (*(metrics.Inodes)).AsInt64()
	if !ok {
		log.Errorf("failed to fetch available inodes for target %s", path)
		return nil, status.Error(codes.Unknown, "failed to fetch available inodes")
	}
	metricInodesFree, ok := (*(metrics.InodesFree)).AsInt64()
	if !ok {
		log.Errorf("failed to fetch free inodes for target: %s", path)
		return nil, status.Error(codes.Unknown, "failed to fetch free inodes")
	}
	metricInodesUsed, ok := (*(metrics.InodesUsed)).AsInt64()
	if !ok {
		log.Errorf("failed to fetch used inodes for target: %s", path)
		return nil, status.Error(codes.Unknown, "failed to fetch used inodes")
	}

	return &csi.NodeGetVolumeStatsResponse{
		Usage: []*csi.VolumeUsage{
			{
				Available: metricAvailable,
				Total:     metricCapacity,
				Used:      metricUsed,
				Unit:      csi.VolumeUsage_BYTES,
			},
			{
				Available: metricInodesFree,
				Total:     metricInodes,
				Used:      metricInodesUsed,
				Unit:      csi.VolumeUsage_INODES,
			},
		},
	}, nil
}

// FSInfo linux returns (available bytes, byte capacity, byte usage, total inodes, inodes free, inode usage, error)
// for the filesystem that path resides upon.
func FsInfo(path string) (int64, int64, int64, int64, int64, int64, error) {
	statfs := &unix.Statfs_t{}
	err := unix.Statfs(path, statfs)
	if err != nil {
		return 0, 0, 0, 0, 0, 0, err
	}

	// Available is blocks available * fragment size
	available := int64(statfs.Bavail) * int64(statfs.Bsize)

	// Capacity is total block count * fragment size
	capacity := int64(statfs.Blocks) * int64(statfs.Bsize)

	// Usage is block being used * fragment size (aka block size).
	usage := (int64(statfs.Blocks) - int64(statfs.Bfree)) * int64(statfs.Bsize)

	inodes := int64(statfs.Files)
	inodesFree := int64(statfs.Ffree)
	inodesUsed := inodes - inodesFree

	return available, capacity, usage, inodes, inodesFree, inodesUsed, nil
}
