package nas

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	cdsNas "github.com/capitalonline/cck-sdk-go/pkg/cck"
	"github.com/capitalonline/cds-csi-driver/pkg/driver/utils"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/kubernetes-csi/drivers/pkg/csi-common"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var (
	// stores the processed pvc: key - pvname, value - *csi.Volume
	processedPvc sync.Map
	// stores the processed pvc: key - pvName, value - "/nfsshare" + "/" + pvName
	pvcMountTargetMap sync.Map
	// stores the processed pvc: key - pvName, value - fileSystemID
	pvcFileSystemIDMap sync.Map
	// stores the processed pvc: key - fileSystemNasID, value - fileSystemIP
	pvcFileSystemIPMap sync.Map
	// pvcFileSystemDeleteMap pvc: key - fileSystemNasID, value - deleting or ok
	pvcFileSystemDeleteMap sync.Map
)

func NewControllerServer(d *NasDriver) *ControllerServer {
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("NewControllerServer:: Failed to create kubernetes config: %v", err)
		utils.SentrySendError(fmt.Errorf("NewControllerServer:: Failed to create kubernetes config: %v", err))
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("NewControllerServer:: Failed to create kubernetes client: %v", err)
		utils.SentrySendError(fmt.Errorf("NewControllerServer:: Failed to create kubernetes client: %v", err))
	}

	return &ControllerServer{
		Client:                  clientset,
		DefaultControllerServer: csicommon.NewDefaultControllerServer(d.csiDriver),
	}
}

func (c *ControllerServer) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	log.Infof("CreateVolume: Starting NFS CreateVolume, req.Name is: %s", req.Name)

	pvName := req.Name

	// step1: check pvc is created or not. return pv directly if created, else do create action
	if value, ok := processedPvc.Load(pvName); ok && value != nil {
		log.Warnf("CreateVolume:: nas Volume %s had been created before, skip: %v", pvName, value)
		return &csi.CreateVolumeResponse{Volume: value.(*csi.Volume)}, nil
	}

	// step2: justify volumeAs type, "subpath" or "filesystem", default is "subpath"
	volOptions := req.GetParameters()
	volumeAs, ok := volOptions["volumeAs"]

	if !ok {
		volumeAs = subpathLiteral
	} else if volumeAs != "filesystem" && volumeAs != "subpath" {
		utils.SentrySendError(fmt.Errorf("Required parameter [parameter.volumeAs] must be [filesystem] or [subpath]"))
		return nil, fmt.Errorf("Required parameter [parameter.volumeAs] must be [filesystem] or [subpath]")
	}

	// step3: create pv
	volToCreate := &csi.Volume{}

	if volumeAs == subpathLiteral {
		log.Debugf("CreateVolume:: nas, provisioning subpath volume, req: %+v", req)

		// step3-subpath-1: parse subpath params
		opts, err := parseVolumeCreateSubpathOptions(req)

		log.Debugf("CreateVolume:: nas, provisioning subpath volume, nfs opts: %+v", opts)

		if err != nil {
			utils.SentrySendError(fmt.Errorf("CreateVolume:: nas, failed to parse subpath create volume req, error is: %s", err.Error()))
			return nil, fmt.Errorf("CreateVolume:: nas, failed to parse subpath create volume req, error is: %s", err.Error())
		}

		// step3-subpath-2: make dir on the nas server
		if err := opts.createNasSubDir(createVolumeRoot, pvName); err != nil {
			utils.SentrySendError(fmt.Errorf("CreateVolume:: nas, failed to create subpath on the nas server, error is: %s", err.Error()))
			return nil, fmt.Errorf("CreateVolume:: nas, failed to create subpath on the nas server, error is: %s", err.Error())
		}

		// step3-subpath-3: assemble the response
		volumeContext := newSubpathVolumeContext(opts, pvName)

		volToCreate = &csi.Volume{
			VolumeId:      pvName,
			CapacityBytes: req.GetCapacityRange().GetRequiredBytes(),
			VolumeContext: volumeContext,
		}
	} else if volumeAs == fileSystemLiteral {
		fileSystemNasID := ""
		fileSystemReqID := ""
		fileSystemNasIP := ""

		// step3-filesystem-1: parse params
		opts, err := parseVolumeCreateFilesystemOptions(req)

		log.Debugf("CreateVolume: nas, provisioning filesystem volume, nfs opts: %+v", opts)

		if err != nil {
			log.Errorf("CreateVolume:: nas, failed to parse filesystem create volume req, error is: %s", err.Error())
			utils.SentrySendError(fmt.Errorf("CreateVolume:: nas, failed to parse filesystem create volume req, error is: %s", err.Error()))
			return nil, fmt.Errorf("CreateVolume:: nas, failed to parse filesystem create volume req, error is: %s", err.Error())
		}

		// step3-filesystem-2: if the pvc mapped fileSystem is already create, skip creating a filesystem
		if value, ok := pvcFileSystemIDMap.Load(pvName); ok && value != "" {
			log.Warnf("CreateVolume: Nfs Volume(%s)'s filesystemID: %s has been Created Already", pvName, value)

			fileSystemNasID = value.(string)

		} else {
			log.Debugf("CreateVolume: nas, provisioning filesystem volume, req: %+v", req)

			// 1-create filesystem,
			storageSize := req.CapacityRange.RequiredBytes / (1024 * 1024 * 1024)
			createFileSystemsNasRes, err := cdsNas.CreateNas(opts.SiteID, pvName[0:29], opts.StorageType, int(storageSize), 1)

			if err != nil {
				log.Errorf("CreateVolume: Create NAS filesystem task api error, error is: %s", err.Error())
				utils.SentrySendError(fmt.Errorf("CreateVolume: Create NAS filesystem task api error, error is: %s", err.Error()))
				return nil, fmt.Errorf("CreateVolume: Create NAS filesystem task api error, error is: %s", err.Error())
			}

			fileSystemReqID = createFileSystemsNasRes.TaskID
			fileSystemNasID = createFileSystemsNasRes.Data.NasID

			// store
			pvcFileSystemIDMap.Store(pvName, fileSystemNasID)

			// storage with nasIP= ""
			pvcFileSystemIPMap.Store(fileSystemNasID, "")

			log.Debugf("CreateVolume: create Nas succeed, fileSystemReqID is: %s, fileSystemNasID is: %s", fileSystemReqID, fileSystemNasID)

			// 2-get Nas creation task status
			err = describeTaskStatus(fileSystemReqID)

			if err != nil {
				log.Errorf("CreateVolume: describeTaskStatus error, err is: %s", err.Error())
				utils.SentrySendError(fmt.Errorf("CreateVolume: describeTaskStatus error, err is: %s", err.Error()))
				return nil, fmt.Errorf("CreateVolume: describeTaskStatus error, err is: %s", err.Error())
			}
			log.Debugf("CreateVolume: create Nas succeed, then to mount Nas to cluster")

			// 3-mount nas then get the nasIP
			mountNasRes, err := cdsNas.MountNas(fileSystemNasID, opts.ClusterID)

			if err != nil {
				log.Errorf("CreateVolume: Create mount Nas task api error, err is: %s", err.Error())
				utils.SentrySendError(fmt.Errorf("CreateVolume: Create mount Nas task api error, err is: %s", err.Error()))
				return nil, fmt.Errorf("CreateVolume: Create mount Nas task api error, err is: %s", err.Error())
			}

			mountReqID := mountNasRes.TaskID
			fileSystemNasIP = mountNasRes.Data.NasIP

			// 4-get mount task status
			err = describeTaskStatus(mountReqID)

			if err != nil {
				log.Errorf("CreateVolume: describeTaskStatus error, err is: %s", err.Error())
				utils.SentrySendError(fmt.Errorf("CreateVolume: describeTaskStatus error, err is: %s", err.Error()))
				return nil, fmt.Errorf("CreateVolume: describeTaskStatus error, err is: %s", err.Error())
			}
			log.Debugf("CreateVolume: mount nas: %s to cluster: %s succeed", fileSystemNasID, opts.ClusterID)

			// storage with nasIP
			pvcFileSystemIPMap.Store(fileSystemNasID, fileSystemNasIP)

			log.Debugf("CreateVolume: Nfs Volume (%s) Successful Created, fileSystemNasID is: %s, FileSystemNasIP is: %s", pvName, fileSystemNasID, fileSystemNasIP)

			// only do create nas storage and mount to cluster, then to return
			return nil, fmt.Errorf("CreateVolume: finish create nas storage and mount it to cluster, next GRPC request will create mountTargetPath")
		}

		// step3-filesystem-3: check if nas mounted to cluster and get nasIP
		if value, ok := pvcFileSystemIPMap.Load(fileSystemNasID); ok && value != "" {
			fileSystemNasIP = value.(string)
		} else {
			log.Warnf("CreateVolume: fileSystemNasID: %s has been created and store in [pvcFileSystemIPMap], but fileSystemNasIP is empty, waiting mount to cluster and get nasIP", fileSystemNasID)
			return nil, fmt.Errorf("CreateVolume: fileSystemNasID: %s, waiting for mount to cluster and get nasIP", fileSystemNasID)
		}

		log.Debugf("CreateVolume: fileSystemNasID is: %s, fileSystemNasIP is: %s, then to create mountTargetPath", fileSystemNasID, fileSystemNasIP)

		// step3-filesystem-4: if mountTarget is already created, skip creating a mountTarget
		mountTargetPath := ""

		if value, ok := pvcMountTargetMap.Load(pvName); ok && value != "" {
			mountTargetPath = value.(string)
			log.Warnf("CreateVolume: Nfs Volume (%s) mountTargetPath: %s has Created Already, skip to get mountTarget's status", pvName, mountTargetPath)
		} else {
			// 1-make dir on the nas server
			mountTargetPath = filepath.Join(defaultNFSRoot, pvName)
			if err := createNasFilesystemSubDir(createVolumeRoot, pvName, fileSystemNasIP); err != nil {
				return nil, fmt.Errorf("CreateVolume:: nas, failed to create mountTargetPath on the nas server, error is: %s", err.Error())
			}

			// 2-store
			pvcMountTargetMap.Store(pvName, mountTargetPath)
			log.Debugf("CreateVolume: Nfs Volume (%s) mountTarget: %s created succeed!", pvName, mountTargetPath)
		}

		// step3-filesystem-5: assemble the response
		volumeContext := map[string]string{}
		volumeContext["volumeAs"] = volumeAs
		volumeContext["fileSystemId"] = fileSystemNasID
		volumeContext["server"] = fileSystemNasIP
		volumeContext["path"] = mountTargetPath
		volumeContext["vers"] = defaultNfsVersion
		volumeContext["deleteNas"] = strconv.FormatBool(opts.DeleteNas)

		volToCreate = &csi.Volume{
			VolumeId:      pvName,
			CapacityBytes: req.GetCapacityRange().GetRequiredBytes(),
			VolumeContext: volumeContext,
		}
	} else {
		log.Errorf("CreateVolume:: nas, unsupported volumeAs: %s, [parameter.volumeAs] must be [filesystem] or [subpath]", volumeAs)
		utils.SentrySendError(fmt.Errorf("CreateVolume:: nas, unsupported volumeAs: %s, [parameter.volumeAs] must be [filesystem] or [subpath]", volumeAs))
		return nil, fmt.Errorf("CreateVolume:: nas, unsupported volumeAs: %s, [parameter.volumeAs] must be [filesystem] or [subpath]", volumeAs)
	}

	// step4: store
	processedPvc.Store(pvName, volToCreate)
	log.Infof("CreateVolume:: nas, succeed provisioned pv %+v:", volToCreate)

	// step5: return
	return &csi.CreateVolumeResponse{Volume: volToCreate}, nil
}

func (c *ControllerServer) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	log.Infof("DeleteVolume:: nas, delete volumeId(pvName) is: %s", req.GetVolumeId())

	pv, err := c.Client.CoreV1().PersistentVolumes().Get(req.VolumeId, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("DeleteVolume:: nas, get Volume: %s from cluster error: %s", req.VolumeId, err.Error())
	}

	// check pv's plug-in, must be not empty
	if pv.Spec.CSI == nil {
		return nil, fmt.Errorf("DeleteVolume:: Nas, Volume Spec with CSI empty: %s, pv: %v", req.VolumeId, pv)
	}
	// get pv's volumeAs, default is subpath
	var volumeAs string
	if value, ok := pv.Spec.CSI.VolumeAttributes["volumeAs"]; !ok {
		volumeAs = subpathLiteral
	} else {
		volumeAs = value
	}
	log.Debugf("DeleteVolume: Nas, volumeAs is: %s", volumeAs)

	// subpath
	if volumeAs == subpathLiteral {
		// step1: get pv's StorageClass info
		if pv.Spec.StorageClassName == "" {
			utils.SentrySendError(fmt.Errorf("DeleteVolume:: nas, volume spec with empty storagecless: %s, Spec: %+v", req.VolumeId, pv.Spec))
			return nil, fmt.Errorf("DeleteVolume:: nas, volume spec with empty storagecless: %s, Spec: %+v", req.VolumeId, pv.Spec)
		}
		sc, err := c.Client.StorageV1().StorageClasses().Get(pv.Spec.StorageClassName, metav1.GetOptions{})
		if err != nil {
			utils.SentrySendError(fmt.Errorf("DeleteVolume:: nas, cannot get storageclass of pv %s: %s", req.VolumeId, err.Error()))
			return nil, fmt.Errorf("DeleteVolume:: nas, cannot get storageclass of pv %s: %s", req.VolumeId, err.Error())
		}
		opts := getDeleteVolumeSubpathOptions(pv, sc)

		pvPath := opts.Path
		if pvPath == "/" || pvPath == "" {
			utils.SentrySendError(fmt.Errorf("DeleteVolume:: nas, pvPath cannot be / or empty"))
			return nil, fmt.Errorf("DeleteVolume:: nas, pvPath cannot be / or empty")
		}

		// step2: nfsPath is the mount point of the nas server
		nasPath := getNasPathFromPvPath(pvPath)
		mountPoint := filepath.Join(deleteVolumeRoot, req.VolumeId+"-delete")
		if err := utils.CreateDir(mountPoint, mountPointMode); err != nil {
			log.Errorf("DeleteVolume:: nas, unable to create directory %s: %s", mountPoint, err)
			utils.SentrySendError(fmt.Errorf("DeleteVolume:: nas, unable to create directory %s: %s", mountPoint, err))
		}
		mntCmd := fmt.Sprintf("mount -t nfs -o vers=%s %s:%s %s", opts.Vers, opts.Server, nasPath, mountPoint)
		if _, err := utils.RunCommand(mntCmd); err != nil {
			log.Errorf("DeleteVolume:: nas, failed to mount %s: %s", mntCmd, err)
			utils.SentrySendError(fmt.Errorf("DeleteVolume:: nas, failed to mount %s: %s", mntCmd, err))
		}
		defer func() {
			if err := utils.Unmount(mountPoint); err != nil {
				log.Errorf("DeleteVolume:: nas, failed to unmount %s: %s", mountPoint, err)
				utils.SentrySendError(fmt.Errorf("DeleteVolume:: nas, failed to unmount %s: %s", mountPoint, err))
			}
		}()

		// do nothing, if the directory is not on nas server
		deletePath := filepath.Join(mountPoint, filepath.Base(pvPath))
		if utils.IsDir(deletePath) {
			if opts.ArchiveOnDelete {
				archivePath := filepath.Join(mountPoint, "archived-"+filepath.Base(pvPath)+time.Now().Format(".2006-01-02-15:04:05"))
				log.Debugf("DeleteVolume:: nas, archiving %s to %s", deletePath, archivePath)
				if err := os.Rename(deletePath, archivePath); err != nil {
					log.Errorf("DeleteVolume:: nas, failed to archive volume %s from path %s to %s with error: %s",
						req.VolumeId, deletePath, archivePath, err.Error())
					utils.SentrySendError(fmt.Errorf("DeleteVolume:: nas, failed to archive volume %s from path %s to %s with error: %s",
						req.VolumeId, deletePath, archivePath, err.Error()))
					return nil, fmt.Errorf("DeleteVolume:: nas, failed to archive volume %s from path %s to %s: %s",
						req.VolumeId, deletePath, archivePath, err.Error())
				}
			} else {
				log.Debugf("DeleteVolume:: nas, deleting %s", deletePath)
				if err := os.RemoveAll(deletePath); err != nil {
					return nil, fmt.Errorf("DeleteVolume:: nas, failed to remove path %s: %s", deletePath, err)
				}
			}
		} else {
			log.Debugf("DeleteVolume:: nas, path %s doesn't exist, skipping", deletePath)
		}
		processedPvc.Delete(req.VolumeId)
		log.Infof("DeleteVolume:: volume %s has been deleted successfully", req.VolumeId)
	}

	// filesystem
	if volumeAs == fileSystemLiteral {
		// step1: check params
		fileSystemNasID := ""
		fileSystemNasIP := ""
		mountTargetPath := ""
		deleteVolume := defaultDeleteNas
		if value, ok := pv.Spec.CSI.VolumeAttributes["deleteNas"]; ok {
			deleteVolume = value
		}
		if deleteVolume == "true" {
			// step2: check pv's params
			if value, ok := pv.Spec.CSI.VolumeAttributes["fileSystemId"]; !ok {
				log.Errorf("DeleteVolume: nas, volumeID is: %s, fileSystemId is empty", req.VolumeId)
				utils.SentrySendError(fmt.Errorf("DeleteVolume: nas, volumeID is: %s, fileSystemId is empty", req.VolumeId))
				return nil, fmt.Errorf("DeleteVolume: nas, volumeID is: %s, fileSystemId is empty", req.VolumeId)
			} else {
				fileSystemNasID = value
				log.Debugf("DeleteVolume: nas, volumeID is: %s, fileSystemId is: %s", req.VolumeId, fileSystemNasID)
			}

			// check if pv is deleted or not
			if value, ok := pvcFileSystemDeleteMap.Load(fileSystemNasID); ok {
				if value == "deleting" {
					log.Debugf("DeleteVolume: the pv has been deleting, ignore this request to avoid repeating delete")
					return nil, fmt.Errorf("DeleteVolume: the pv have been deleting, ignore this request to avoid repeating delete")
				} else if value == "ok" {
					log.Debugf("DeleteVolume: pv deleting process finished")
					pvcFileSystemDeleteMap.Delete(fileSystemNasID)
					return &csi.DeleteVolumeResponse{}, nil
				}

			}

			// set pv deleting status
			pvcFileSystemDeleteMap.Store(fileSystemNasID, "deleting")

			// check pv nasIP
			if value, ok := pv.Spec.CSI.VolumeAttributes["server"]; ok {
				fileSystemNasIP = value
				log.Debugf("DeleteVolume: nas, server is: %s", fileSystemNasIP)
			} else {
				log.Errorf("DeleteVolume: nas server is empty, CSI is: %v", pv.Spec.CSI)
				utils.SentrySendError(fmt.Errorf("DeleteVolume: nas server is empty, CSI is: %v", pv.Spec.CSI))
				return nil, fmt.Errorf("DeleteVolume: nas server is empty, CSI is: %v", pv.Spec.CSI)
			}

			// check pv path
			if value, ok := pv.Spec.CSI.VolumeAttributes["path"]; ok {
				mountTargetPath = value
				log.Debugf("DeleteVolume: nas, path is: %s", mountTargetPath)
			} else {
				log.Errorf("DeleteVolume: path is empty, CSI is: %v", pv.Spec.CSI)
				utils.SentrySendError(fmt.Errorf("DeleteVolume: path is empty, CSI is: %v", pv.Spec.CSI))
				return nil, fmt.Errorf("DeleteVolume: path is empty, CSI is: %v", pv.Spec.CSI)
			}

			// step3: delete pv data in NAS storage
			if err := deleteNasFilesystemSubDir(deleteVolumeRoot, mountTargetPath, fileSystemNasIP); err != nil {
				return nil, fmt.Errorf("DeleteVolume: nas, failed to delete mountTargetPath on the nas server, error is: %s", err.Error())
			}
			log.Debugf("DeleteVolume: nas, delete pv path succeed in NAS storage")

			// step4: Unmount NAS storage from cluster
			unMountNasRes, err := cdsNas.UnMountNas(fileSystemNasID)
			if err != nil {
				log.Errorf("DeleteVolume: cdsNas.UnMountNas api error, err is: %s", err)
				utils.SentrySendError(fmt.Errorf("DeleteVolume: cdsNas.UnMountNas api error, err is: %s", err))
			}

			// get unmount task status
			unMountNasTaskID := unMountNasRes.TaskID
			log.Debugf("DeleteVolume: cdsNas.UnMountNas, taskID is:%s", unMountNasTaskID)

			err = describeTaskStatus(unMountNasTaskID)

			if err != nil {
				log.Errorf("DeleteVolume: describeTaskStatus error, err is: %s", err.Error())
				utils.SentrySendError(fmt.Errorf("DeleteVolume: describeTaskStatus error, err is: %s", err.Error()))
				return nil, fmt.Errorf("CreateVolume: describeTaskStatus error, err is: %s", err.Error())
			}
			log.Infof("DeleteVolume: UnMountNas NAS storage succeed")

			// step5: delete NAS storage
			_, err = cdsNas.DeleteNas(fileSystemNasID)

			if err != nil {
				log.Errorf("DeleteVolume: cdsNas.DeleteNas api error, err is: %s", err)
				utils.SentrySendError(fmt.Errorf("DeleteVolume: cdsNas.DeleteNas api error, err is: %s", err))
				return nil, fmt.Errorf("DeleteVolume:cdsNas.DeleteNas api error, err is: %s", err)
			}

			log.Infof("DeleteVolume: delete NAS storage succeed!")

			// step6: clean processedPvc, pvcMountTargetMap, pvcFileSystemIPMap, pvcFileSystemIDMap, pvcFileSystemDeleteMap
			processedPvc.Delete(req.VolumeId)
			pvcMountTargetMap.Delete(req.VolumeId)
			pvcFileSystemIPMap.Delete(fileSystemNasID)
			pvcFileSystemIDMap.Delete(req.VolumeId)
			pvcFileSystemDeleteMap.Store(fileSystemNasID, "ok")
			log.Debugf("clean processedPvc, pvcMountTargetMap, pvcFileSystemIDMap, pvcFileSystemIPMap, pvcFileSystemDeleteMap record")

			// step7: response
			log.Infof("DeleteVolume:: Nas volume(%s)'s NAS storage and mountTargetPath have been deleted successfully", req.VolumeId)
		} else {
			// retain pv's filesystem NAS storage
			log.Warnf("DeleteVolume: Nas volume(%s) Filesystem's deleteVolume is [false], remain nas storage and pv data", req.VolumeId)
		}
	}

	return &csi.DeleteVolumeResponse{}, nil
}

func (c ControllerServer) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	for _, capability := range req.VolumeCapabilities {
		if capability.GetAccessMode().GetMode() != csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER {
			return &csi.ValidateVolumeCapabilitiesResponse{Message: ""}, nil
		}
	}
	return &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
			VolumeCapabilities: req.VolumeCapabilities,
		},
	}, nil
}

func (c ControllerServer) ControllerExpandVolume(context.Context, *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func describeTaskStatus(taskID string) error {

	log.Debugf("describeTaskStatus: taskID is: %s", taskID)

	for i := 1; i < 120; i++ {

		res, err := cdsNas.DescribeTaskStatus(taskID)

		if err != nil {
			log.Errorf("describeTaskStatus: cdsNas.DescribeTaskStatus api error, err is: %s", err)
			utils.SentrySendError(fmt.Errorf("describeTaskStatus: cdsNas.DescribeTaskStatus api error, err is: %s", err))
			return fmt.Errorf("apiError")
		}

		if res.Data.Status == "finish" {
			log.Debugf("describeTaskStatus: task succeed")
			return nil
		} else if res.Data.Status == "doing" {
			log.Debugf("describeTaskStatus: task:%s is running, sleep 10s", taskID)
			time.Sleep(10 * time.Second)
		} else if res.Data.Status == "error" {
			log.Debugf("describeTaskStatus: task is error")
			return fmt.Errorf("taskError")
		}
	}

	return fmt.Errorf("describeTaskStatus: task time out, running more than 20 minutes")
}
