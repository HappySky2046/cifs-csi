package cifs

import (
	"os"
	"path"

	"github.com/golang/glog"

	"github.com/container-storage-interface/spec/lib/go/csi/v0"
	"github.com/kubernetes-csi/drivers/pkg/csi-common"
)

const (
	PluginFolder = "/var/lib/kubelet/plugins/csi-cifsplugin"
	Version      = "0.3.0"
)

type cifsDriver struct {
	driver *csicommon.CSIDriver

	server csicommon.NonBlockingGRPCServer

	is *identityServer
	ns *nodeServer
	cs *controllerServer

	caps   []*csi.VolumeCapability_AccessMode
	cscaps []*csi.ControllerServiceCapability
}

func NewCifsDriver() *cifsDriver {
	return &cifsDriver{}
}

func NewIdentityServer(d *csicommon.CSIDriver) *identityServer {
	return &identityServer{
		DefaultIdentityServer: csicommon.NewDefaultIdentityServer(d),
	}
}

func NewControllerServer(d *csicommon.CSIDriver) *controllerServer {
	return &controllerServer{
		DefaultControllerServer: csicommon.NewDefaultControllerServer(d),
	}
}

func NewNodeServer(d *csicommon.CSIDriver) *nodeServer {
	return &nodeServer{
		DefaultNodeServer: csicommon.NewDefaultNodeServer(d),
	}
}

func createPersistentStorage(persistentStoragePath string) error {
	return os.MkdirAll(persistentStoragePath, os.FileMode(0755))
}

func (fs *cifsDriver) Init(driverName, nodeId string) {
	glog.Infof("Driver: %v version: %v", driverName, Version)

	if err := createPersistentStorage(path.Join(PluginFolder, "controller")); err != nil {
		glog.Fatalf("failed to create persistent storage for controller: %v", err)
	}

	if err := createPersistentStorage(path.Join(PluginFolder, "node")); err != nil {
		glog.Fatalf("failed to create persistent storage for node: %v", err)
	}

	if err := createPersistentStorage(path.Join(PluginFolder, "controller", "plugin-cache")); err != nil {
		glog.Fatalf("failed to create persistent storage for controllercache: %v", err)
	}

	if err := loadControllerCache(); err != nil {
		glog.Errorf("cifs: failed to read volume cache: %v", err)
	}

	fs.driver = csicommon.NewCSIDriver(driverName, Version, nodeId)
	if fs.driver == nil {
		glog.Fatalln("Failed to initialize CSI driver")
	}

	fs.driver.AddControllerServiceCapabilities([]csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
	})

	fs.driver.AddVolumeCapabilityAccessModes([]csi.VolumeCapability_AccessMode_Mode{
		csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER,
	})

	fs.is = NewIdentityServer(fs.driver)
	fs.ns = NewNodeServer(fs.driver)
	fs.cs = NewControllerServer(fs.driver)

	fs.server = csicommon.NewNonBlockingGRPCServer()
}

func (fs *cifsDriver) Start(endpoint string) {
	fs.server.Start(endpoint, fs.is, fs.cs, fs.ns)
	fs.server.Wait()
}

func (fs *cifsDriver) Stop() {
	fs.server.Stop()
}
