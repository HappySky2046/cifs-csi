package cifs

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/golang/glog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/kubernetes/pkg/util/mount"
	"k8s.io/kubernetes/pkg/volume/util"

	"github.com/container-storage-interface/spec/lib/go/csi/v0"
	"github.com/kubernetes-csi/drivers/pkg/csi-common"
)

type nodeServer struct {
	cr *credentials
	*csicommon.DefaultNodeServer

	mounter mount.Interface
}

type volumeID string

func (ns *nodeServer) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	glog.Infof("stage")
	var err error

	if err = validateNodeStageVolumeRequest(req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	if ns.mounter == nil {
		ns.mounter = mount.New("")
	}
	// Configuration

	stagingTargetPath := req.GetStagingTargetPath()
	volId := volumeID(req.GetVolumeId())
	glog.Infof("cifs: volume %s is trying to create and mount %s", volId, stagingTargetPath)

	notMnt, err := ns.mounter.IsLikelyNotMountPoint(stagingTargetPath)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(stagingTargetPath, 0750); err != nil {
				return nil, status.Error(codes.Internal, err.Error())
			}
			notMnt = true
		} else {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}
	if !notMnt {
		glog.Infof("cifs: volume %s is already mounted to %s, skipping", volId, stagingTargetPath)
		return &csi.NodeStageVolumeResponse{}, nil
	}

	ns.cr, err = getUserCredentials(req.GetNodeStageSecrets())

	if err != nil {
		return nil, fmt.Errorf("failed to get user credentials from node stage secrets: %v", err)
	}
	if ns.cr.username == "" || ns.cr.password == "" {
		return nil, fmt.Errorf("TODO: need to auth")
	}

	mo := []string{}
	mo = append(mo, fmt.Sprintf("username=%s", ns.cr.username))
	mo = append(mo, fmt.Sprintf("password=%s", ns.cr.password))

	s := req.GetVolumeAttributes()["server"]
	ep := req.GetVolumeAttributes()["share"]
	if s == "" || ep == "" {
		return nil, fmt.Errorf("TODO: need server or endpoint")
	}
	source := fmt.Sprintf("//%s/%s", s, ep)

	err = ns.mounter.Mount(source, stagingTargetPath, "cifs", mo)
	if err != nil {
		if os.IsPermission(err) {
			return nil, status.Error(codes.PermissionDenied, err.Error())
		}
		if strings.Contains(err.Error(), "invalid argument") {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &csi.NodeStageVolumeResponse{}, nil
}

func (ns *nodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	glog.Infof("publish")

	if err := validateNodePublishVolumeRequest(req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	if ns.mounter == nil {
		ns.mounter = mount.New("")
	}

	targetPath := req.GetTargetPath()
	volId := req.GetVolumeId()

	notMnt, err := ns.mounter.IsLikelyNotMountPoint(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(targetPath, 0750); err != nil {
				return nil, status.Error(codes.Internal, err.Error())
			}
			notMnt = true
		} else {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}

	if !notMnt {
		glog.Infof("cifs: volume %s is already bind-mounted to %s", volId, targetPath)
		return &csi.NodePublishVolumeResponse{}, nil
	}

	mo := []string{"bind"}
	if req.GetReadonly() {
		mo = append(mo, "ro")
	}
	if err = ns.mounter.Mount(req.GetStagingTargetPath(), req.GetTargetPath(), "cifs", mo); err != nil {
		glog.Errorf("failed to bind-mount volume %s: %v", volId, err)
		return nil, status.Error(codes.Internal, err.Error())
	}

	glog.Infof("cifs: successfully bind-mounted volume %s to %s", volId, targetPath)

	return &csi.NodePublishVolumeResponse{}, nil
}

const (
	username = "username"
	password = "password"
)

type credentials struct {
	username string
	password string
}

func getCredentials(u, p string, secrets map[string]string) (*credentials, error) {
	var (
		c  = &credentials{}
		ok bool
	)

	if c.username, ok = secrets[username]; !ok {
		return nil, fmt.Errorf("missing username in secrets")
	}

	if c.password, ok = secrets[password]; !ok {
		return nil, fmt.Errorf("missing password in secrets")
	}

	return c, nil
}

func getUserCredentials(secrets map[string]string) (*credentials, error) {
	return getCredentials(username, password, secrets)
}

func (ns *nodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	if err := validateNodeUnpublishVolumeRequest(req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	if ns.mounter == nil {
		ns.mounter = mount.New("")
	}

	targetPath := req.GetTargetPath()
	notMnt, err := ns.mounter.IsLikelyNotMountPoint(targetPath)

	if err != nil {
		if os.IsNotExist(err) {
			return nil, status.Error(codes.NotFound, "Targetpath not found")
		} else {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}
	if notMnt {
		return nil, status.Error(codes.NotFound, "Volume not mounted")
	}

	err = util.UnmountPath(req.GetTargetPath(), mount.New(""))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (ns *nodeServer) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	if err := validateNodeUnstageVolumeRequest(req); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	stagingTargetPath := req.GetStagingTargetPath()
	// Unmount the volume
	if err := util.UnmountPath(stagingTargetPath, mount.New("")); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	glog.Infof("cifs: successfully umounted volume %s from %s", req.GetVolumeId(), stagingTargetPath)

	if err := os.Remove(stagingTargetPath); err != nil {
		glog.Warningf("cifs: failed to clean up %s: %v", stagingTargetPath, err)
	}
	return &csi.NodeUnstageVolumeResponse{}, nil
}
