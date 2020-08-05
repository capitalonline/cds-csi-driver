package nas

import (
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/capitalonline/cds-csi-driver/pkg/driver/utils"
	"github.com/container-storage-interface/spec/lib/go/csi"
	log "github.com/sirupsen/logrus"
	core "k8s.io/api/core/v1"
	storage "k8s.io/api/storage/v1"

	cdsNas "github.com/capitalonline/cck-sdk-go/pkg/cck"
)

func (opts *NfsOpts) parsNfsOpts() error {
	opts.versNormalization()
	// parse path
	if opts.Path == "" {
		log.Warnf("nas, path is empty, using default root %s", defaultNFSRoot)
		opts.Path = defaultNFSRoot
	}
	// remove / if path end with /;
	for opts.Path != "/" && strings.HasSuffix(opts.Path, "/") {
		opts.Path = opts.Path[0 : len(opts.Path)-1]
	}
	if !strings.HasPrefix(opts.Path, defaultNFSRoot) {
		return fmt.Errorf("the path format is illegal, need start with %s, given: %s", defaultNFSRoot, opts.Path)
	}

	// parse options, config defaults for nas based on vers
	if opts.Options == "" {
		opts.Options = opts.getDefaultMountOptions()
	} else if strings.ToLower(opts.Options) == "none" {
		opts.Options = ""
	}
	return nil
}

func (opts *NfsOpts) versNormalization() {
	if opts.Vers == "" {
		opts.Vers = "4.0"
	}
	if opts.Vers == "3.0" {
		opts.Vers = "3"
	} else if opts.Vers == "4" {
		opts.Vers = "4.0"
	}
}

func (opts *NfsOpts) nfsV4() bool {
	return strings.HasPrefix(opts.Vers, "4")
}

func (opts *NfsOpts) getDefaultMountOptions() string {
	if opts.nfsV4() {
		return defaultV4Opts
	} else {
		return defaultV3Opts
	}
}

func parseMountOptionsField(mntOptions []string) (vers string, opts string) {
	if len(mntOptions) > 0 {
		mntOptionsStr := strings.Join(mntOptions, ",")
		// mntOptions should re-split, as some like ["a,b,c", "d"]
		mntOptionsList := strings.Split(mntOptionsStr, ",")
		var tmpOptionsList []string

		if strings.Contains(mntOptionsStr, "vers=3.0") {
			for _, tmpOptions := range mntOptionsList {
				if tmpOptions != "vers=3.0" {
					tmpOptionsList = append(tmpOptionsList, tmpOptions)
				}
			}
			vers, opts = "", strings.Join(tmpOptionsList, ",")
		} else if strings.Contains(mntOptionsStr, "vers=3") {
			for _, tmpOptions := range mntOptionsList {
				if tmpOptions != "vers=3" {
					tmpOptionsList = append(tmpOptionsList, tmpOptions)
				}
			}
			vers, opts = "3", strings.Join(tmpOptionsList, ",")
		} else if strings.Contains(mntOptionsStr, "vers=4.0") {
			for _, tmpOptions := range mntOptionsList {
				if tmpOptions != "vers=4.0" {
					tmpOptionsList = append(tmpOptionsList, tmpOptions)
				}
			}
			vers, opts = "4.0", strings.Join(tmpOptionsList, ",")
		} else if strings.Contains(mntOptionsStr, "vers=4.1") {
			for _, tmpOptions := range mntOptionsList {
				if tmpOptions != "vers=4.1" {
					tmpOptionsList = append(tmpOptionsList, tmpOptions)
				}
			}
			vers, opts = "4.1", strings.Join(tmpOptionsList, ",")
		} else {
			vers, opts = "", strings.Join(mntOptions, ",")
		}
	}
	return
}

func newPublishOptions(req *csi.NodePublishVolumeRequest) *PublishOptions {
	opts := &PublishOptions{}
	opts.NodePublishPath = req.GetTargetPath()
	for key, value := range req.VolumeContext {
		if key == "server" {
			opts.Server = value
		} else if key == "path" {
			opts.Path = value
		} else if key == "vers" {
			opts.Vers = value
		} else if key == "mode" {
			opts.Mode = value
		} else if key == "options" {
			opts.Options = value
		} else if key == "modeType" {
			opts.ModeType = value
		} else if key == "volumeAs" {
			opts.VolumeAs = value
		}else if key == "allowShared" {
			allowed, err := strconv.ParseBool(value)
			if err != nil {
				opts.AllowSharePath = false
			}
			opts.AllowSharePath = allowed
		}
	}
	return opts
}

func newSubpathVolumeContext(opts *VolumeCreateSubpathOptions, pvName string) map[string]string {
	ctx := make(map[string]string)
	ctx["volumeAs"] = opts.VolumeAs
	ctx["server"] = opts.Server
	ctx["path"] = filepath.Join(opts.Path, pvName)
	ctx["mode"] = opts.Mode
	ctx["modeType"] = opts.ModeType
	ctx["options"] = opts.Options
	ctx["vers"] = opts.Vers
	return ctx
}

func parsePublishOptions(req *csi.NodePublishVolumeRequest) (*PublishOptions, error) {
	opts := newPublishOptions(req)

	// set volumeAs to default "subpath"
	if opts.VolumeAs == "" {
		opts.VolumeAs = "subpath"
	}

	if opts.NodePublishPath == "" {
		return nil, errors.New("mountPath is empty")
	}

	if opts.Server == "" {
		return nil, errors.New("host is empty, should input nas domain")
	}

	if err := opts.parsNfsOpts(); err != nil {
		return nil, err
	}

	// version/options settings in mountOptions field will overwrite the options
	if req.VolumeCapability != nil && req.VolumeCapability.GetMount() != nil {
		mntOptions := req.VolumeCapability.GetMount().MountFlags
		vers, options := parseMountOptionsField(mntOptions)
		if vers != "" {
			if opts.Vers != "" {
				log.Warnf("nas, Vers(%s) (in volumeAttributes) is ignored as Vers(%s) also configured in mountOptions", opts.Vers, vers)
			}
			opts.Vers = vers
		}
		if options != "" {
			if opts.Options != "" {
				log.Warnf("nas, Options(%s) (in volumeAttributes) is ignored as Options(%s) also configured in mountOptions", opts.Options, options)
			}
			opts.Options = options
		}
	}

	if !utils.ServerReachable(opts.Server, nasPortNumber, dialTimeout) {
		log.Errorf("nas, cannot connect to nas host: %s", opts.Server)
		return nil, fmt.Errorf("nas, cannot connect to nas host: %s", opts.Server)
	}

	return opts, nil
}

func newVolumeCreateSubpathOptions(param map[string]string) *VolumeCreateSubpathOptions {
	opts := &VolumeCreateSubpathOptions{}
	opts.VolumeAs = param["volumeAs"]
	opts.Servers = param["servers"]
	opts.Server = param["server"]
	opts.Path = param["path"]
	opts.Vers = param["vers"]
	opts.Options = param["options"]
	opts.Mode = param["mode"]
	opts.ModeType = param["modeType"]
	opts.Strategy = param["strategy"]

	return opts
}

func newVolumeCreateFilesystemOptions(param map[string]string) *VolumeCreateFilesystemOptions {
	opts := &VolumeCreateFilesystemOptions{}
	opts.VolumeAs = param["volumeAs"]
	opts.ProtocolType = param["protocolType"]
	opts.StorageType = param["storageType"]
	opts.SiteID = param["siteID"]
	opts.ClusterID = param["clusterID"]
	value, ok := param["deleteNas"]
	if !ok {
		opts.DeleteNas = false
	}else {
		value = strings.ToLower(value)
		if value == "true" {
			opts.DeleteNas = true
		} else {
			opts.DeleteNas = false
		}
	}
	return opts
}

func parseVolumeCreateFilesystemOptions(req *csi.CreateVolumeRequest) (*VolumeCreateFilesystemOptions, error) {
	opts := newVolumeCreateFilesystemOptions(req.GetParameters())
	// protocolType
	if opts.ProtocolType == "" {
		log.Warnf("input ProtocolType is none, default set it to NFS")
		opts.ProtocolType = "NFS"
	} else if opts.ProtocolType != "NFS" {
		return nil, fmt.Errorf("Required parameter [parameter.protocolType] must be [NFS]")
	}

	// storageType
	if opts.StorageType == "" {
		opts.StorageType = "high_disk"
	} else if opts.StorageType != "high_disk" {
		return nil, fmt.Errorf("Required parameter [parameter.storageType] must be [high_disk] ")
	}

	// siteID and clusterID
	if opts.SiteID == "" || opts.ClusterID == "" {
		log.Errorf("siteID or clusterID is empty, it must be not empty", opts.SiteID)
		return nil, fmt.Errorf("siteID or clusterID is empty, it must be not empty")
	}
	return opts, nil
}

func parseVolumeCreateSubpathOptions(req *csi.CreateVolumeRequest) (*VolumeCreateSubpathOptions, error) {

	opts := newVolumeCreateSubpathOptions(req.GetParameters())

	if opts.Server == "" && opts.Servers == "" {
		return nil, fmt.Errorf("nas, fatel error, server or servers is missing on volume as subpath")
	}

	var serverSlice []string

	if opts.Servers != "" {
		serverSlice = strings.Split(opts.Servers, ",")
	}
	if opts.Server != "" {
		serverSlice = append(serverSlice, strings.Join([]string{opts.Server, strings.TrimPrefix(opts.Path, "/")}, "/"))
	}
	log.Infof("serverSlice is: %s", serverSlice)

	servers := ParseServerList(serverSlice)

	var nfsServer *NfsServer

	switch len(servers) {
	case 0:
		return nil, fmt.Errorf("nas, fatel error, [server or servers is missing ] or [servers usage all > 80] on volume as subpath")
	case 1:
		opts.Server = servers[0].Address
		opts.Path = servers[0].Path
	default:
		if opts.Strategy == "" {
			opts.Strategy = "RoundRobin"
		}
		// uniqueSelectString is to flag function like sc name function
		var uniqueSelectString string
		for _, v := range serverSlice {
			uniqueSelectString = strings.Join([]string{uniqueSelectString, strings.TrimSpace(v)}, ",")
		}
		// delete additional ","
		strings.TrimPrefix(uniqueSelectString, ",")
		nfsServer = SelectServer(servers, uniqueSelectString, strings.ToLower(opts.Strategy))
		if nfsServer == nil {
			log.Errorf("provision volume: failed to choose a server using strategy %s, use the first one instead", opts.Strategy)
			opts.Server = servers[0].Address
			opts.Path = servers[0].Path
		}
		opts.Server = nfsServer.Address
		opts.Path = nfsServer.Path
	}

	if err := opts.parsNfsOpts(); err != nil {
		return nil, err
	}

	if opts.ModeType == "" {
		opts.ModeType = "non-recursive"
	}

	return opts, nil
}

// optimizeNfsSetting config tcp_slot_table_entries to 128 to improve NFS client performance
func optimizeNasSetting() {
	updateNasConfig := false
	if !utils.FileExisted(sunRPCFile) {
		updateNasConfig = true
	} else {
		chkCmd := fmt.Sprintf("cat %s | grep tcp_slot_table_entries | grep 128 | grep -v grep | wc -l", sunRPCFile)
		out, err := utils.RunCommand(chkCmd)
		if err != nil {
			log.Warnf("nas, update Nas system config check error: %s", err.Error())
			return
		}
		if strings.TrimSpace(out) == "0" {
			updateNasConfig = true
		}
	}

	if updateNasConfig {
		upCmd := fmt.Sprintf("echo \"options sunrpc tcp_slot_table_entries=128\" >> %s && echo \"options sunrpc tcp_max_slot_table_entries=128\" >> %s && sysctl -w sunrpc.tcp_slot_table_entries=128", sunRPCFile, sunRPCFile)
		_, err := utils.RunCommand(upCmd)
		if err != nil {
			log.Warnf("nss, update nas system config error: %s", err.Error())
			return
		}
		log.Warnf("nas, successfully update Nas system config")
	}
}

func mountNasVolume(opts *PublishOptions, volumeId string) error {
	var versStr string

	if opts.Options == "" {
		versStr = opts.Vers
	} else {
		versStr = fmt.Sprintf("%s,%s", opts.Vers, opts.Options)
	}

	var serverMountPoint string
	if opts.VolumeAs == "subpath" {
		if opts.AllowSharePath {
			// share path
			serverMountPoint = opts.Path
		} else {
			// not share path
			if strings.Contains(opts.Path, "pvc-") && len(opts.Path) > 32 {
				// dynamic pv
				serverMountPoint = opts.Path
			} else {
				// static pv
				serverMountPoint = filepath.Join(opts.Path, volumeId)
			}
		}
	} else if opts.VolumeAs == "filesystem" {
		// never share for filesystem
		serverMountPoint = opts.Path
	} else {
		log.Errorf("mountNasVolume:: volumeAs type is not [subpath] or [filesystem], not support")
		return fmt.Errorf("mountNasVolume:: volumeAs type is not [subpath] or [filesystem], not support")
	}

	mntCmd := fmt.Sprintf("mount -t nfs -o vers=%s %s:%s %s", versStr, opts.Server, serverMountPoint, opts.NodePublishPath)
	log.Infof("mountNasVolume:: mntCmd is: %s", mntCmd)

	_, err := utils.RunCommand(mntCmd)
	if err != nil && opts.Path != "/" {
		if strings.Contains(err.Error(), "No such file or directory") ||
			strings.Contains(err.Error(), "access denied by server while mounting") {
			subDir := volumeId
			if opts.AllowSharePath {
				subDir = ""
			}
			if err := opts.createNasSubDir(publishVolumeRoot, subDir); err != nil {
				return fmt.Errorf("nas, create subpath error: %s", err.Error())
			}
			if _, err := utils.RunCommand(mntCmd); err != nil {
				log.Errorf("nas, mount nfs fail after creating the sub directory: %s", err.Error())
				return err
			}
		} else {
			return err
		}
	} else if err != nil {
		return err
	}

	log.Infof("nas, mount nfs successful with command: %s", mntCmd)
	return nil
}

func (opts *NfsOpts) createNasSubDir(mountRoot, subDir string) error {
	log.Infof("nas, running creatNasSubDir: root: %s, path: %s, subDir:%s", mountRoot, opts.Path, subDir)

	localMountPath := filepath.Join(mountRoot, subDir)
	fullPath := filepath.Join(localMountPath, strings.TrimPrefix(opts.Path, defaultNFSRoot), subDir)

	// unmount the volume if it has been mounted
	log.Infof("nas, unmount localMountPath if is mounted: %s", localMountPath)

	if utils.Mounted(localMountPath) {
		if err := utils.Unmount(localMountPath); err != nil {
			log.Errorf("nas, failed to unmount already mounted path %s: %s", localMountPath, err)
		}
	}

	log.Infof("nas, creating localMountPath dir: %s", localMountPath)

	if err := utils.CreateDir(localMountPath, mountPointMode); err != nil {
		return fmt.Errorf("nas, create localMountPath %s err: %s", localMountPath, err.Error())
	}

	// mount localMountPath to remote nfs server
	mntCmd := fmt.Sprintf("mount -t nfs -o vers=%s %s:%s %s", opts.Vers, opts.Server, defaultNFSRoot, localMountPath)

	log.Infof("nas, mount for sub dir: %s", mntCmd)

	if _, err := utils.RunCommand(mntCmd); err != nil {
		return fmt.Errorf("nas, failed to localMountPath %s: %s", mntCmd, err.Error())
	}

	// create sub directory, which makes the folder on the remote nfs server at the same time
	log.Infof("nas, creating fullPath: %s", fullPath)

	if err := utils.CreateDir(fullPath, mountPointMode); err != nil {
		return fmt.Errorf("nas, create sub directory err: " + err.Error())
	}
	defer os.RemoveAll(localMountPath)

	log.Infof("nas, changing mode for %s", fullPath)

	if err := os.Chmod(fullPath, mountPointMode); err != nil {
		log.Errorf("nas, failed to change the mode of %s to %d", fullPath, mountPointMode)
	}

	// unmount the local path after the remote folder is created
	log.Infof("nas, unmount dir after the dir creation: %s", fullPath)

	if err := utils.Unmount(localMountPath); err != nil {
		log.Errorf("nas, failed to unmount path %s: %s", fullPath, err)
	}

	log.Infof("nas, create sub directory successful: %s", opts.Path)

	return nil
}

func createNasFilesystemSubDir(localMountPath, subDir, fileSystemNasIP string) error {
	log.Infof("nas, running createNasFilesystemSubDir")

	createFullPath := filepath.Join(localMountPath, subDir)

	log.Infof("localMountPath is: %s, createFullPath is: %s, pvPath is:%s", localMountPath, createFullPath, subDir)

	// unmount the localMountPath if mounted
	log.Infof("nas, unmount localMountPath if mounted: %s", localMountPath)

	if utils.Mounted(localMountPath) {
		if err := utils.Unmount(localMountPath); err != nil {
			log.Errorf("nas, failed to unmount already mounted path: %s, err is: %s", localMountPath, err.Error())
		}
	}

	log.Infof("nas, creating localMountPath dir: %s", localMountPath)

	if err := utils.CreateDir(localMountPath, mountPointMode); err != nil {
		return fmt.Errorf("nas, create localMountPath %s err: %s", localMountPath, err.Error())
	}

	// mount remote nfs server /nfsshare to localMountPath
	mntCmd := fmt.Sprintf("mount -t nfs -o vers=%s %s:%s %s", defaultNfsVersion, fileSystemNasIP, defaultNFSRoot, localMountPath)

	log.Infof("nas, mntCmd is: %s", mntCmd)

	if _, err := utils.RunCommand(mntCmd); err != nil {
		return fmt.Errorf("nas, failed to run mntCmd: %s, err is:  %s", mntCmd, err.Error())
	}

	// create sub directory, which makes the folder on the remote nfs server at the same time
	log.Infof("nas, creating createFullPath: %s", createFullPath)

	if err := utils.CreateDir(createFullPath, mountPointMode); err != nil {
		return fmt.Errorf("nas, create sub directory err: " + err.Error())
	}

	// finally delete localMountPath
	defer os.RemoveAll(localMountPath)

	log.Infof("nas, changing mode for %s", createFullPath)

	if err := os.Chmod(createFullPath, mountPointMode); err != nil {
		log.Errorf("nas, failed to change the mode of %s to %d", createFullPath, mountPointMode)
	}

	// unmount the localMountPath after the remote folder is created
	log.Infof("nas, unmount localMountPath: %s", localMountPath)

	if err := utils.Unmount(localMountPath); err != nil {
		log.Errorf("nas, failed to unmount localMountPath, error is: %s", err)
	}

	log.Infof("nas, create pv path: %s, in remote server's /nfsshare successfully", subDir)

	return nil
}

func deleteNasFilesystemSubDir(mountRoot, subDir, fileSystemNasIP string) error {

	// log.Infof("nas, running createNasFilesystemSubDir, mountRoot is: %s, subDir is:%s", mountRoot, subDir)

	// unmount the volume if it has been mounted
	// log.Infof("nas, unmount mountRoot if is mounted: %s", mountRoot)
	if utils.Mounted(mountRoot) {
		if err := utils.Unmount(mountRoot); err != nil {
			log.Errorf("nas, failed to unmount already mounted path: %s, err is: %s", mountRoot, err)
		}
	}

	// log.Infof("nas, creating mountRoot dir: %s", mountRoot)
	if err := utils.CreateDir(mountRoot, mountPointMode); err != nil {
		return fmt.Errorf("nas, create mountRoot %s err: %s", mountRoot, err.Error())
	}

	// mount mountRoot to remote nfs server
	mntCmd := fmt.Sprintf("mount -t nfs -o vers=%s %s:%s %s", defaultNfsVersion, fileSystemNasIP, defaultNFSRoot, mountRoot)
	log.Infof("nas, mntCmd is: %s", mntCmd)

	if _, err := utils.RunCommand(mntCmd); err != nil {
		return fmt.Errorf("nas, failed to mountRoot %s: %s", mntCmd, err.Error())
	}

	// delete pv's path
	deleteDir := mountRoot + strings.TrimPrefix(subDir, "/nfsshare")
	log.Infof("nas, delete pv path is: %s", deleteDir)

	if err := os.RemoveAll(deleteDir); err != nil {
		log.Errorf("nas, delete pv path error, err is: %s", err)
	}

	// log.Infof("nas, delete pv path succeed in NAS storage")
	defer os.RemoveAll(mountRoot)

	// unmount the local path after the remote folder is created
	// log.Infof("nas, unmount dir after the dir creation: %s", mountRoot)
	if err := utils.Unmount(mountRoot); err != nil {
		log.Errorf("nas, failed to unmount path %s: %s", mountRoot, err)
	}

	return nil
}

func changeNasMode(opts *PublishOptions) {
	if opts.Mode != "" && opts.Path != "/" {
		wg1 := sync.WaitGroup{}
		wg1.Add(1)

		go func(*sync.WaitGroup) {
			cmd := fmt.Sprintf("chmod %s %s", opts.Mode, opts.NodePublishPath)
			if opts.ModeType == "recursive" {
				cmd = fmt.Sprintf("chmod -R %s %s", opts.Mode, opts.NodePublishPath)
			}
			if _, err := utils.RunCommand(cmd); err != nil {
				log.Errorf("nas, chmod cmd fail: %s %s", cmd, err)
			} else {
				log.Infof("nas, chmod cmd success: %s", cmd)
			}
			wg1.Done()
		}(&wg1)

		timeout := 1 // 1s
		if utils.WaitTimeout(&wg1, timeout) {
			log.Infof("nas, chmod runs more than %ds, continues to run in background: %s", timeout, opts.NodePublishPath)
		}
	}
}

func getNasPathFromPvPath(pvPath string) (nasPath string) {
	tmpPath := pvPath
	if strings.HasSuffix(pvPath, "/") {
		tmpPath = pvPath[0 : len(pvPath)-1]
	}
	pos := strings.LastIndex(tmpPath, "/")
	nasPath = pvPath[0:pos]
	if nasPath == "" {
		nasPath = "/"
	}
	return
}

func getDeleteVolumeSubpathOptions(pv *core.PersistentVolume, sc *storage.StorageClass) *DeleteVolumeSubpathOptions {
	opts := &DeleteVolumeSubpathOptions{}
	opts.Server = pv.Spec.CSI.VolumeAttributes["server"]
	opts.Path = pv.Spec.CSI.VolumeAttributes["path"]
	opts.Vers = pv.Spec.CSI.VolumeAttributes["vers"]
	if archiveOnDelete, ok := sc.Parameters["archiveOnDelete"]; ok {
		archiveBool, err := strconv.ParseBool(archiveOnDelete)
		if err != nil {
			log.Errorf("nas, failed to get archieveOnDelete value, setting to true by default: %s", err.Error())
			opts.ArchiveOnDelete = true
		} else {
			opts.ArchiveOnDelete = archiveBool
		}
	}
	return opts
}

// parse ServerList that support multi servers in one SC
func ParseServerList(serverList []string) []*NfsServer {
	// delete usage > 80 server
	idleServerList := DeleteUsageFullServers(serverList)
	if len(idleServerList) == 0 {
		return nil
	}

	// params
	servers := make([]*NfsServer, 0)
	for _, server := range idleServerList {

		addrPath := strings.SplitN(strings.TrimSpace(server), "/", 2)
		if len(addrPath) < 2 {
			continue
		}
		if addrPath[0] == "" {
			continue
		}
		addr := strings.TrimSpace(addrPath[0])
		path := strings.TrimSpace(addrPath[1])
		if path == "" {
			path = defaultNfsPath
		}
		servers = append(servers, &NfsServer{Address: addr, Path: filepath.Join("/", path)})
	}
	return servers
}

func SelectServer(servers []*NfsServer, uniqueSelectString string, strategy string) *NfsServer {
	switch strategy {
	case "roundrobin":
		return SelectServerRoundRobin(servers, uniqueSelectString)
	case "random":
		return SelectServerRandom(servers)
	default:
		return servers[0]
	}
}

func SelectServerRoundRobin(servers []*NfsServer, uniqueSelectString string) *NfsServer {
	RRLock.Lock()
	count := RR[uniqueSelectString]
	RR[uniqueSelectString] = count + 1
	RRLock.Unlock()
	length := uint(len(servers))
	if length == 0 {
		return nil
	}

	return servers[count%length]
}

func SelectServerRandom(servers []*NfsServer) *NfsServer {
	length := len(servers)
	if length == 0 {
		return nil
	}
	return servers[rand.Intn(length)]
}

func DeleteUsageFullServers(serverList []string) []string{
	var tmpServers []string

	for k, v := range serverList {
		addrPath := strings.SplitN(strings.TrimSpace(v), "/", 2)
		addr := strings.TrimSpace(addrPath[0])
		log.Infof("cdsNas.DescribeMountPoint: addr is: %s", addr)
		res, err := cdsNas.DescribeNasUsage(os.Getenv(defaultClusterID), addr)
		if err != nil {
			continue
		}
		log.Infof("cdsNas.DescribeMountPoint: res is: %v", res)

		if res != nil {
			if  usageInt, _ := strconv.Atoi(strings.TrimSuffix(res.Data.NasInfo[0].UsageRate, "%")); usageInt < defaultNasUsage {
				tmpServers = append(tmpServers, serverList[k])
			}
		}
	}
	return tmpServers
}