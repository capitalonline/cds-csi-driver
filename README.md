# cds-csi-driver

A Container Storage Interface (CSI) Driver for CapitalOnline(aka. CDS) cloud. This CSI plugin allows you to use CDS storage with the kubernetes cluster hosted or managed by CDS.

It currently supports CDS File Storage(NAS).
The support for the Block Storage and Object Storage will be added soon.

## To deploy

To deploy the CSI NAS driver to your k8s cluster, simply run:
```bash
kubectl apply -f https://raw.githubusercontent.com/capitalonline/cds-csi-driver/master/deploy/nas/deploy.yaml
```

## To run tests

1. Make sure that you have a Kubernetes cluster accessible with `kubectl`
2. Provision a nfs server with exports `/nfsshare *(insecure,rw,async,no_root_squash,fsid=1000)`
3. Run `make test-prerequisite` to build the image and deploy the driver to your k8s cluster
4. Run `sudo NFS=<your nfs server ip> make test`

## To use the NAS driver
Examples can be found here [here](!https://github.com/capitalonline/cds-csi-driver/tree/master/example/nas)
### To use static provisioned PV
Please add the following definition to to PersistentVolume yaml file:
```yaml
spec:
  csi:
    driver: nas.csi.cds.net
    volumeHandle: <name of the pv>
    volumeAttributes:
      server: "140.210.75.195"
      path: "/nfsshare/nas-csi-cds-pv"
```
Description:

`driver`: should always be `nas.csi.cds.net`

`volumeHandle`: set the same value of the pv's name(defined in `meta.name`)

`volumeAttributes`: defined as below:

|Key|Value|Required|Description|
|:----:|:-----------|:---:|:----|
|server|e.g. "140.210.75.195"|yes|the mount point of the nfs server,can be found on NAS product list|
|path|e.g. "/nfsshare/pvname"|yes|the path that the pv will use on the nfs server. It must start with `/nfsshare`. Please make sure each pv use a different path.|
|vers|`3` or `4.0`|no| the protocol version to use for the nfs mount. if not provided, `4.0` will be used by default
|options|e.g. `noresvport`|no| options for the nfs mount. If not provided it will be set to `noresvport` for `vers=4.0` and `noresvport,nolock,tcp` for `vers=3`|
|mode|e.g. `0777`|no| to customize the mode of the path created on nfs server. If not provided, the server's default mask will be used|
|modeType|e.g `recursive` or `non-recursive`|no| This tells the driver if the chmod should be done recursively or not. Only works when `mode` is specified. Default to `non-recursive`|
|allowShared| `"true"` or `"false"`|no | default to `"false`. If set, the directory for the pv will be exactly the same as path. Otherwise, the directory will be `path`+`pv id`. This is useful if you need two pvs share the same storage|

### To use dynamic provisioned PV
please add the following definition to the StorageClass yaml file:
```yaml
provisioner: nas.csi.cds.net
parameters:
  volumeAs: subpath
  server: "140.210.75.195"
  path: "/nfsshare"
  vers: "4.0"
  options: noresvport
  archiveOnDelete: "true"
```
Description:

`provisioner`: should always be `nas.csi.cds.net`

`parameters`: Defined as below:

`parametes.volumAs`: `subpath` or `system`. `subpath` means each pv will be provisioned on a specified NAS server, while `system` means each PV will be provision with a new NAS Storage and use the storage exclusively.
***Note***: only `subpath` is supported at the moment.

Other parameters are defined below, when the `parametes.volumAs` is `subpath`:

|Key|Value|Required|Description|
|:----:|:-----------|:---:|:----|
|server|e.g. "140.210.75.195"|yes|the mount point of the nfs server,can be found on NAS product list|
|path|e.g. "/nfsshare"|yes|the root path that the pv will dynamically provisioned on the NAS server. It must start with `/nfsshare`. |
|vers|`3` or `4.0`|no| the protocol version to use for the nfs mount. if not provided, `4.0` will be used by default
|options|e.g. `noresvport`|no| options for the nfs mount. If not provided it will be set to `noresvport` for `vers=4.0` and `noresvport,nolock,tcp` for `vers=3`|
|mode|e.g. `0777`|no| to customize the mode of the path created on nfs server. If not provided, the server's default mask will be used
|modeType|e.g `recursive` or `non-recursive`|no| This tells the driver if the chmod should be done recursively or not. Only works when `mode` is specified. Default to `non-recursive`
|archiveOnDelete|`"true"` or `"false"`|no| only works when the `reclaim police` is `delete`. if `true`, the content of the pv will be archived on nfs instead of be deleted. Default to `false`|
