package oss

import (
	"context"
	"errors"
	"fmt"
	"github.com/capitalonline/cds-csi-driver/pkg/driver/utils"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/kubernetes-csi/drivers/pkg/csi-common"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strings"
)

func NewNodeServer(d *OssDriver) *NodeServer {
	return &NodeServer{
		DefaultNodeServer: csicommon.NewDefaultNodeServer(d.csiDriver),
	}
}

func (n *NodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	log.Infof("NodePublishVolume:: starting mount oss with req: %+v", req)
	opts := &PublishOptions{}
	opts.NodePublishPath = req.GetTargetPath()
	if opts.NodePublishPath == "" {
		log.Errorf("oss mountPath is necessary but input empty")
		utils.SentrySendError(fmt.Errorf("oss mountPath is necessary but input empty"))
		return nil, errors.New("oss mountPath is necessary but input empty")
	}
	for key, value := range req.VolumeContext {
		key = strings.ToLower(key)
		if key == "bucket" {
			opts.Bucket = strings.TrimSpace(value)
		} else if key == "url" {
			opts.URL = strings.TrimSpace(value)
		} else if key == "path" {
			opts.Path = strings.TrimSpace(value)
		} else if key == "akid" {
			opts.AkID = strings.TrimSpace(value)
		} else if key == "aksecret" {
			opts.AkSecret = strings.TrimSpace(value)
		} else if key == "authtype" {
			opts.AuthType = strings.ToLower(strings.TrimSpace(value))
		}
	}
	// check parameters
	if err := opts.parsOssOpts(); err != nil {
		return nil, err
	}
	log.Debugf("NodePublishVolume:: parsed PublishOptions options: %+v", opts)

	// directly return if the target mountPath has been mounted
	if utils.Mounted(opts.NodePublishPath) {
		log.Debugf("NodePublishVolume:: oss, mountPath: %s is mounted", opts.NodePublishPath)
		return &csi.NodePublishVolumeResponse{}, nil
	}

	// Save ak file for s3fs in default
	opts.AuthType = AuthTypeDefault
	// save AK and AKS
	if opts.AuthType == "saveAkFile" {
		// save ak file: bucket:ak_id:ak_secret to /etc/s3pass
		if err := opts.saveOssCredential(CredentialFile); err != nil {
			log.Debugf("save ak file: bucket:ak_id:ak_secret failed")
			return nil, err
		}
	} else {
		log.Errorf("AuthType verify error, AuthType is only support %s", AuthTypeDefault)
		utils.SentrySendError(fmt.Errorf("AuthType verify error, AuthType is only support %s", AuthTypeDefault))
		return nil, errors.New("AuthType verify error, not support, it should to be saveAkFile")
	}

	var mntCmd string
	log.Debugf("NodePublishVolume:: Start mount source [%s:%s] to [%s]", opts.Bucket, opts.Path, opts.NodePublishPath)
	mntCmd = fmt.Sprintf("s3fs %s:%s %s -o passwd_file=%s -o url=%s %s", opts.Bucket, opts.Path, opts.NodePublishPath, CredentialFile, opts.URL, defaultOtherOpts)
	log.Debugf("mntCmd is: %s", mntCmd)
	if _, err := utils.RunCommand(mntCmd); err != nil {
		log.Errorf("Mount oss bucket to mountPath failed, error is: %s", err)
		utils.SentrySendError(fmt.Errorf("Mount oss bucket to mountPath failed, error is: %s", err))
		return nil, err
	}
	// recheck oss mount result
	if !utils.Mounted(opts.NodePublishPath) {
		log.Errorf("Remote bucket path [%s:%s] is not exist, please create it firstly", opts.Bucket, opts.Path)
		utils.SentrySendError(fmt.Errorf("Remote bucket path [%s:%s] is not exist, please create it firstly", opts.Bucket, opts.Path))
		errMsg := fmt.Sprintf("Remote bucket path [%s:%s] is not exist, please create it firstly", opts.Bucket, opts.Path)
		return nil, errors.New(errMsg)
	}
	log.Infof("NodePublishVolume:: Mount Oss successful, volumeID:%s, oss: [%s:%s], targetPath:%s", req.VolumeId, opts.NodePublishPath, opts.Path, opts.NodePublishPath)
	return &csi.NodePublishVolumeResponse{}, nil
}

func (n *NodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	log.Infof("NodeUnpublishVolume:: starting Umount Oss Volume %s at path %s", req.VolumeId, req.TargetPath)

	// skip the unmount if the path is not mounted
	mountPoint := req.TargetPath
	if !utils.Mounted(mountPoint) {
		log.Warnf("NodeUnpublishVolume:: oss, unmount mountpoint not found, skipping: %s", mountPoint)
		return &csi.NodeUnpublishVolumeResponse{}, nil
	}

	// unmount the volume, use force umount on network not reachable or no other pod used
	unmoutCmd := fmt.Sprintf("umount %s", mountPoint)
	if _, err := utils.RunCommand(unmoutCmd); err != nil {
		return nil, fmt.Errorf("NodeUnpublishVolume:: oss, Umount oss bucket fail: %s", err.Error())
	}

	log.Infof("NodeUnpublishVolume:: Unmount oss Successfully on: %s", mountPoint)
	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (n *NodeServer) NodeStageVolume(context.Context, *csi.NodeStageVolumeRequest) (
	*csi.NodeStageVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (n *NodeServer) NodeUnstageVolume(context.Context, *csi.NodeUnstageVolumeRequest) (
	*csi.NodeUnstageVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (n *NodeServer) NodeExpandVolume(context.Context, *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}
