package nas

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/capitalonline/cds-csi-driver/pkg/driver/utils"
	"github.com/container-storage-interface/spec/lib/go/csi"
	log "github.com/sirupsen/logrus"
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
)

func parseMountOptions(req *csi.NodePublishVolumeRequest) (options *MountOptions, err error) {
	// parse parameters
	mountPath := req.GetTargetPath()
	opts := &MountOptions{}
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

	// check parameters
	if mountPath == "" {
		return nil, errors.New("mountPath is empty")
	}
	if opts.Server == "" {
		return nil, errors.New("host is empty, should input nas domain")
	}
	// check network connection
	if !utils.ServerReachable(opts.Server, nasPortNumber, dialTimeout) {
		log.Errorf("nas, cannot connect to nas host: %s", opts.Server)
		return nil, errors.New("nas, cannot connect to nas host: " + opts.Server)
	}

	// parse nfs version
	if opts.Vers == "" {
		opts.Vers = "3"
	}
	if opts.Vers == "3.0" {
		opts.Vers = "3"
	} else if opts.Vers == "4" {
		opts.Vers = "4.0"
	}
	nfsV4 := false
	if strings.HasPrefix(opts.Vers, "4") {
		nfsV4 = true
	}

	// parse path
	if opts.Path == "" {
		if nfsV4 {
			opts.Path = defaultV4Path
		} else {
			opts.Path = defaultV3Path
		}
	}
	// remove / if path end with /;
	if opts.Path != "/" && strings.HasSuffix(opts.Path, "/") {
		opts.Path = opts.Path[0 : len(opts.Path)-1]
	}

	if opts.Path == "/" && opts.Mode != "" {
		return nil, errors.New("nas: root directory cannot set mode: " + opts.Mode)
	}
	if !strings.HasPrefix(opts.Path, "/") {
		return nil, errors.New("the path format is illegal")
	}

	// parse options, config defaults for aliyun nas
	if opts.Options == "" {
		if nfsV4 {
			opts.Options = defaultV4Opts
		} else {
			opts.Options = defaultV3Opts
		}
	} else if strings.ToLower(opts.Options) == "none" {
		opts.Options = ""
	}
	return opts, nil
}

func parseMountOptionsField(mntOptions []string) (vers string, opts string) {
	if len(mntOptions) > 0 {
		mntOptionsStr := strings.Join(mntOptions, ",")
		// mntOptions should re-split, as some like ["a,b,c", "d"]
		mntOptionsList := strings.Split(mntOptionsStr, ",")
		tmpOptionsList := []string{}

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

func mountNasVolume(opts *MountOptions, volumeId string) error {
	if !utils.FileExisted(opts.MountPath) {
		utils.CreateDir(opts.MountPath)
	}
	mntCmd := fmt.Sprintf("mount -t nfs -o vers=%s %s:%s %s", opts.Vers, opts.Server, opts.Path, opts.MountPath)
	if opts.Options != "" {
		mntCmd = fmt.Sprintf("mount -t nfs -o vers=%s,%s %s:%s %s", opts.Vers, opts.Options, opts.Server, opts.Path, opts.MountPath)
	}
	_, err := utils.RunCommand(mntCmd)
	if err != nil && opts.Path != "/" {
		if strings.Contains(err.Error(), "reason given by server: No such file or directory") ||
			strings.Contains(err.Error(), "access denied by server while mounting") {
			if err := createNasSubDir(opts, volumeId); err != nil {
				log.Errorf("nas, create subPath error: %s", err.Error())
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

func createNasSubDir(opts *MountOptions, volumeId string) error {
	// create local mount path
	nasTmpPath := filepath.Join(nasTempMountPath, volumeId)
	if err := utils.CreateDir(nasTmpPath); err != nil {
		log.Infof("nas, create temp Directory err: " + err.Error())
		return err
	}
	if utils.Mounted(nasTmpPath) {
		if err := utils.Unmount(nasTmpPath); err != nil {
			log.Errorf("nas, failed to unmount path %s:", nasTmpPath, err)
		}
	}

	// mount local path to remote nfs server
	usePath := opts.Path
	mntCmd := fmt.Sprintf("mount -t nfs -o vers=%s %s:%s %s", opts.Vers, opts.Server, "/", nasTmpPath)
	_, err := utils.RunCommand(mntCmd)
	if err != nil {
		if strings.Contains(err.Error(), "reason given by server: No such file or directory") ||
			strings.Contains(err.Error(), "access denied by server while mounting") {
			if strings.HasPrefix(opts.Path, defaultV3Path+"/") {
				usePath = strings.TrimPrefix(usePath, defaultV3Path)
				mntCmd = fmt.Sprintf("mount -t nfs -o vers=%s %s:%s %s", opts.Vers, opts.Server, defaultV3Path, nasTmpPath)
				_, err := utils.RunCommand(mntCmd)
				if err != nil {
					log.Errorf("nas, mount to temp directory %s fail: %s ", usePath, err.Error())
					return err
				}
			} else {
				log.Errorf("nas, maybe use version 3, but path not start with %s: %s", defaultV3Path, err.Error())
				return err
			}
		} else {
			log.Errorf("nas, Mount to temp directory fail: %s" + err.Error())
			return err
		}
	}

	// create sub directory, which makes the folder on the remote nfs server at the same time
	subPath := path.Join(nasTmpPath, usePath)
	if err := utils.CreateDir(subPath); err != nil {
		log.Errorf("nas, create sub directory err: " + err.Error())
		return err
	}

	// unmount the local path after the remote folder is created
	utils.Unmount(nasTmpPath)
	log.Infof("nas, create sub directory successful: %s", opts.Path)
	return nil
}

func changeNasMode(opts *MountOptions) {
	if opts.Mode != "" && opts.Path != "/" {
		wg1 := sync.WaitGroup{}
		wg1.Add(1)

		go func(*sync.WaitGroup) {
			cmd := fmt.Sprintf("chmod %s %s", opts.Mode, opts.MountPath)
			if opts.ModeType == "recursive" {
				cmd = fmt.Sprintf("chmod -R %s %s", opts.Mode, opts.MountPath)
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
			log.Infof("nas, chmod runs more than %ds, continues to run in background: %s", timeout, opts.MountPath)
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
