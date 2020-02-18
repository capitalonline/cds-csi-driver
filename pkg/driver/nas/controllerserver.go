package nas

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

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
)

const (
	MountPointRootPathForCreatingVolume = "/csi-nas-mount-root"
	MountPointRootPathForDeletingVolume = "/csi-nas-unmount-root"
	MountPointMode                      = 0777
)

type ControllerServer struct {
	*csicommon.DefaultControllerServer
	Client *kubernetes.Clientset
}

type VolumeCreateSubpathOptions struct {
	NfsOpts
	VolumeAs string
}

type DeleteVolumeSubpathOptions struct {
	Server          string
	Path            string
	Vers            string
	ArchiveOnDelete bool
}

func NewControllerServer(d *NasDriver) *ControllerServer {
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("NewControllerServer:: Failed to create kubernetes config: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("NewControllerServer:: Failed to create kubernetes client: %v", err)
	}

	return &ControllerServer{
		Client:                  clientset,
		DefaultControllerServer: csicommon.NewDefaultControllerServer(d.csiDriver),
	}
}

func (c *ControllerServer) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	pvName := req.Name
	log.Infof("CreateVolume:: starting nas Provisioning, %s, %v", pvName, req)
	var ok bool
	var volumeAs = subpathLiteral

	// directly return is the pvc has been processed
	if value, ok := processedPvc.Load(pvName); ok && value != nil {
		log.Infof("CreateVolume:: nas Volume %s had been created before, skip: %v", pvName, value)
		return &csi.CreateVolumeResponse{Volume: value.(*csi.Volume)}, nil
	}

	if volumeAs, ok = req.GetParameters()["volumeAs"]; !ok {
		volumeAs = subpathLiteral
	}

	volToCreate := &csi.Volume{}
	if volumeAs == subpathLiteral {
		opts, err := parseVolumeCreateSubpathOptions(req)
		log.Infof("CreateVolume:: nas, provisioning subpath volume: %+v", opts)
		if err != nil {
			log.Errorf("CreateVolume:: nas, failed to parse create volume req: %s", err.Error())
		}

		mountPoint := filepath.Join(MountPointRootPathForCreatingVolume, pvName)
		// create the local mount folder in the container
		if err = utils.CreateDir(mountPoint, MountPointMode); err != nil {
			log.Errorf("CreateVolume:: nas, unable to create directory: %s", mountPoint)
			return nil, fmt.Errorf("CreateVolume:: nas, unable to create directory: %s", mountPoint)
		}
		// make dir on the nas server
		if err != opts.createNasSubDir(pvName) {
			return nil, fmt.Errorf("CreateVolume:: nas, failed to create subpath on the nas server: %s", err)
		}
		// assemble the response
		volumeContext := newSubpathVolumeContext(opts, pvName)
		volToCreate = &csi.Volume{
			VolumeId:      pvName,
			CapacityBytes: req.GetCapacityRange().GetRequiredBytes(),
			VolumeContext: volumeContext,
		}
	} else if volumeAs == systemLiteral {
		return nil, errors.New("CreateVolume:: nas, volumeAs \"system\" is not supported yet")
	} else {
		return nil, fmt.Errorf("CreateVolume:: nas, unsupported volumeAs: %s", volumeAs)
	}
	processedPvc.Store(pvName, volToCreate)
	log.Infof("CreateVolume:: nas, successfully provisioned pv %+v:", volToCreate)
	return &csi.CreateVolumeResponse{Volume: volToCreate}, nil
}

func (c *ControllerServer) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	log.Infof("DeleteVolume:: nas, starting deleting volume %s", req.GetVolumeId())

	pv, err := c.Client.CoreV1().PersistentVolumes().Get(req.VolumeId, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("DeleteVolume:: nas, get Volume: %s from cluster error: %s", req.VolumeId, err.Error())
	}

	// get PV volumeAs
	if pv.Spec.CSI == nil {
		return nil, fmt.Errorf("DeleteVolume:: nas, Volume Spec with CSI empty: %s, pv: %v", req.VolumeId, pv)
	}
	var volumeAs string
	if v, ok := pv.Spec.CSI.VolumeAttributes["volumeAs"]; !ok {
		volumeAs = subpathLiteral
	} else {
		volumeAs = v
	}

	if volumeAs == subpathLiteral {
		if pv.Spec.StorageClassName == "" {
			return nil, fmt.Errorf("DeleteVolume:: nas, volume spec with empty storagecless: %s, Spec: %+v", req.VolumeId, pv.Spec)
		}
		sc, err := c.Client.StorageV1().StorageClasses().Get(pv.Spec.StorageClassName, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("DeleteVolume:: nas, cannot get storageclass of pv %s: %s", req.VolumeId, err.Error())
		}
		opts := getDeleteVolumeSubpathOptions(pv, sc)

		pvPath := opts.Path
		if pvPath == "/" || pvPath == "" {
			log.Errorf("DeleteVolume:: nas, pvPath cannot be / or empty")
			return nil, errors.New("DeleteVolume:: nas, pvPath cannot be / or empty")
		}

		// nfsPath is the mount point of the nas server
		nasPath := getNasPathFromPvPath(pvPath)
		mountPoint := filepath.Join(MountPointRootPathForDeletingVolume, req.VolumeId+"-delete")
		if err := utils.CreateDir(mountPoint, MountPointMode); err != nil {
			log.Errorf("DeleteVolume:: nas, unable to create directory %s: %s", mountPoint, err)
		}
		mntCmd := fmt.Sprintf("mount -t nfs -o vers=%s %s:%s %s", opts.Vers, opts.Server, nasPath, mountPoint)
		if _, err := utils.RunCommand(mntCmd); err != nil {
			log.Errorf("DeleteVolume:: nas, failed to mount %s: %s", mntCmd, err)
		}
		defer func() {
			if err := utils.Unmount(mountPoint); err != nil {
				log.Errorf("DeleteVolume:: nas, failed to unmount %s: %s", mountPoint, err)
			}
		}()

		// do nothing, if the directory is not on nas server
		deletePath := filepath.Join(mountPoint, filepath.Base(pvPath))
		if utils.IsDir(deletePath) {
			if opts.ArchiveOnDelete {
				archivePath := filepath.Join(mountPoint, "archived-"+filepath.Base(pvPath)+time.Now().Format(".2006-01-02-15:04:05"))
				if err := os.Rename(deletePath, archivePath); err != nil {
					log.Errorf("DeleteVolume:: nas, failed to archive volume %s from path %s to %s with error: %s",
						req.VolumeId, deletePath, archivePath, err.Error())
					return nil, fmt.Errorf("DeleteVolume:: nas, failed to archive volume %s from path %s to %s: %s",
						req.VolumeId, deletePath, archivePath, err.Error())
				}
			} else {
				if err := os.RemoveAll(deletePath); err != nil {
					return nil, fmt.Errorf("DeleteVolume:: nas, failed to remove path %s: %s", deletePath, err)
				}
			}
		}

		log.Infof("DeleteVolume:: nas, path %s doesn't exist, skipping")
		processedPvc.Delete(req.VolumeId)
		return &csi.DeleteVolumeResponse{}, nil
	} else if volumeAs == systemLiteral {
		return nil, errors.New("DeleteVolume:::: nas, volumeAs \"system\" is not supported yet")
	} else {
		return nil, fmt.Errorf("CreateVolume:: nas, unsupported volumeAs: %s", volumeAs)
	}
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
