package nas

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/capitalonline/cds-csi-driver/pkg/driver/utils"
	"github.com/container-storage-interface/spec/lib/go/csi"
	log "github.com/sirupsen/logrus"
	core "k8s.io/api/core/v1"
	storage "k8s.io/api/storage/v1"
)

const (
	sunRPCFile       = "/etc/modprobe.d/sunrpc.conf"
	nasTempMountPath = "/mnt/cds_mnt/k8s_nas/temp"
	defaultV3Path    = "/nfsshare"
	defaultV4Path    = "/"
	defaultV3Opts    = "noresvport,nolock,tcp"
	defaultV4Opts    = "noresvport"
	nasPortNumber    = "2049"
	dialTimeout      = time.Duration(3) * time.Second
	subpathLiteral   = "subpath"
	systemLiteral    = "system"
)

type NfsOpts struct {
	Server   string
	Path     string
	Vers     string
	Mode     string
	ModeType string
	Options  string
}

func (opts *NfsOpts) parsNfsOpts() error {
	opts.versNormalization()
	// parse path
	if opts.Path == "" {
		opts.Path = opts.getDefaultMountPath()
	}
	// remove / if path end with /;
	for opts.Path != "/" && strings.HasSuffix(opts.Path, "/") {
		opts.Path = opts.Path[0 : len(opts.Path)-1]
	}
	if !strings.HasPrefix(opts.Path, "/") {
		return errors.New("the path format is illegal")
	}

	// parse options, config defaults for nas based on vers
	if opts.Options == "" {
		opts.Options = opts.getDefaultMountOptions()
	} else if strings.ToLower(opts.Options) == "none" {
		opts.Options = ""
	}

	if opts.Path == "/" && opts.Mode != "" {
		return errors.New("nas: root directory cannot set mode: " + opts.Mode)
	}
	return nil
}

func (opts *NfsOpts) versNormalization() {
	if opts.Vers == "" {
		opts.Vers = "3"
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

func (opts *NfsOpts) getDefaultMountPath() string {
	if opts.nfsV4() {
		return defaultV4Path
	} else {
		return defaultV3Path
	}
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
	return ctx
}

func parsePublishOptions(req *csi.NodePublishVolumeRequest) (*PublishOptions, error) {
	opts := newPublishOptions(req)

	if opts.NodePublishPath == "" {
		return nil, errors.New("mountPath is empty")
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

	if opts.Server == "" {
		return nil, errors.New("host is empty, should input nas domain")
	}
	if !utils.ServerReachable(opts.Server, nasPortNumber, dialTimeout) {
		log.Errorf("nas, cannot connect to nas host: %s", opts.Server)
		return nil, errors.New("nas, cannot connect to nas host: " + opts.Server)
	}

	return opts, nil
}

func newVolumeCreateSubpathOptions(param map[string]string) *VolumeCreateSubpathOptions {
	opts := &VolumeCreateSubpathOptions{}
	opts.VolumeAs = subpathLiteral
	opts.Server = param["server"]
	opts.Path = param["path"]
	opts.Vers = param["vers"]
	opts.Options = param["options"]
	opts.Mode = param["mode"]
	opts.ModeType = param["modeType"]

	return opts
}

func parseVolumeCreateSubpathOptions(req *csi.CreateVolumeRequest) (*VolumeCreateSubpathOptions, error) {
	opts := newVolumeCreateSubpathOptions(req.GetParameters())
	if opts.Server == "" {
		return nil, fmt.Errorf("nas, fatel error, server is missing on volume as subpath")
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

	mntCmd := fmt.Sprintf("mount -t nfs -o vers=%s %s:%s %s", versStr, opts.Server, opts.Path, opts.NodePublishPath)
	_, err := utils.RunCommand(mntCmd)
	if err != nil && opts.Path != "/" {
		if strings.Contains(err.Error(), "reason given by server: No such file or directory") ||
			strings.Contains(err.Error(), "access denied by server while mounting") {
			if err := opts.createNasSubDir(volumeId); err != nil {
				log.Errorf("nas, create subpath error: %s", err.Error())
				return err
			}
			if _, err := utils.RunCommand(mntCmd); err != nil {
				log.Errorf("nas, mount nfs sub directory fail: %s", err.Error())
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

func (opts *NfsOpts) createNasSubDir(volumeDirName string) error {
	// create local mount path
	nasLocalTmpPath := filepath.Join(nasTempMountPath, volumeDirName)
	if err := utils.CreateDir(nasLocalTmpPath, MountPointMode); err != nil {
		log.Infof("nas, create temp Directory err: " + err.Error())
		return err
	}
	// umount the nasLocalTmpPath if it has been mounted
	if utils.Mounted(nasLocalTmpPath) {
		if err := utils.Unmount(nasLocalTmpPath); err != nil {
			log.Errorf("nas, failed to unmount path %s: %s", nasLocalTmpPath, err)
		}
	}
	// mount nasLocalTmpPath to remote nfs server
	//usePath := opts.Path
	mntCmd := fmt.Sprintf("mount -t nfs -o vers=%s %s:%s %s", opts.Vers, opts.Server, opts.getDefaultMountPath(), nasLocalTmpPath)
	if _, err := utils.RunCommand(mntCmd); err != nil {
		log.Errorf("nas, failed mount to temp directory fail: %s" + err.Error())
		return err
	}
	// create sub directory, which makes the folder on the remote nfs server at the same time
	fullPath := filepath.Join(nasTempMountPath, strings.TrimPrefix(opts.Path, opts.getDefaultMountPath()))
	if err := utils.CreateDir(fullPath, MountPointMode); err != nil {
		log.Errorf("nas, create sub directory err: " + err.Error())
		return err
	}

	if err := os.Chmod(fullPath, MountPointMode); err != nil {
		log.Errorf("nas, failed to change the mode of %s to %d", fullPath, MountPointMode)
	}

	// unmount the local path after the remote folder is created
	if err := utils.Unmount(nasLocalTmpPath); err != nil {
		log.Errorf("nas, failed to unmount path %s: %s", fullPath, err)
	}
	log.Infof("nas, create sub directory successful: %s", opts.Path)
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

func parseServerHost(mountPoint string) string {
	getNasServerPath := fmt.Sprintf("findmnt %s | grep %s | grep -v grep | awk '{print $2}'", mountPoint, mountPoint)
	serverAndPath, _ := utils.RunCommand(getNasServerPath)
	serverAndPath = strings.TrimSpace(serverAndPath)

	serverInfoPartList := strings.Split(serverAndPath, ":")
	if len(serverInfoPartList) != 2 {
		log.Warnf("nas, failed to parse server host from mount point: %s", mountPoint)
		return ""
	}
	return serverInfoPartList[0]
}

func testIfNoOtherNasUser(nfsServer, mountPoint string) bool {
	checkCmd := fmt.Sprintf("mount | grep -v %s | grep %s | grep -v grep | wc -l", mountPoint, nfsServer)
	if checkOut, err := utils.RunCommand(checkCmd); err != nil {
		return false
	} else if strings.TrimSpace(checkOut) != "0" {
		return false
	}
	return true
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
	if _, ok := sc.Parameters["archieveOnDelete"]; ok {
		archiveBool, err := strconv.ParseBool("archiveOnDelete")
		if err != nil {
			log.Errorf("nas, failed to get archieveOnDelete value, setting to true by default")
			opts.ArchiveOnDelete = true
		}
		opts.ArchiveOnDelete = archiveBool
	}
	return opts
}
