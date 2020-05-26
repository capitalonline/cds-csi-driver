# cds-csi-driver

A Container Storage Interface (CSI) Driver for CapitalOnline(aka. CDS) cloud. This CSI plugin allows you to use CDS storage with the kubernetes cluster hosted or managed by CDS.

It currently supports CDS File Storage(NAS) and CDS Object Storage Service(OSS).
The support for the Block Storage will be added soon.

## To deploy

To deploy the CSI NAS driver to your k8s cluster, simply run:
```bash
kubectl apply -f https://raw.githubusercontent.com/capitalonline/cds-csi-driver/master/deploy/nas/deploy.yaml
```

To deploy the CSI OSS driver to your k8s cluster, simply run:

```bash
kubectl apply -f https://raw.githubusercontent.com/capitalonline/cds-csi-driver/master/deploy/oss/deploy.yaml
```

## To run tests

NAS:

1. Make sure that you have a Kubernetes cluster accessible with `kubectl`
2. Provision a nfs server with exports `/nfsshare *(insecure,rw,async,no_root_squash,fsid=1000)`
3. Run `make test-prerequisite` to build the image and deploy the driver to your k8s cluster
4. Run `sudo NFS=<your nfs server ip> make test`

OSS:

1. Make sure that you have a Kubernetes cluster accessible with `kubectl`
2. Register a Storage Service in CDS 
3. Run `make test-prerequisite` to build the image and deploy the driver to your k8s cluster
4. Run `make oss-test`

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
      server: "140.210.75.196"
      path: "/nfsshare/nas-csi-cds-pv"
```
Description:

`driver`: should always be `nas.csi.cds.net`

`volumeHandle`: set the same value of the pv's name(defined in `meta.name`)

`volumeAttributes`: defined as below:

|Key|Value|Required|Description|||
|:----:|:-----------|:---:|:----|------|------|
|server|e.g. "140.210.75.195"|yes|the mount point of the nfs server,can be found on NAS product list|||
|path|e.g. "/nfsshare/division1"|yes|the path that the pv will use on the nfs server. It must start with `/nfsshare`. Each pv will use a separate directory(e.g `/nfsshare/division1/volumeHandleValue"`when `allowShared` is `false`. Otherwsise, the path of pv will be the same to `path`, which means all PVs with the same path will share data|||
|vers|`3` or `4.0`|no| the protocol version to use for the nfs mount. if not provided, `4.0` will be used by default
|options|e.g. `noresvport`|no| options for the nfs mount. If not provided it will be set to `noresvport` for `vers=4.0` and `noresvport,nolock,tcp` for `vers=3`|
|mode|e.g. `0777`|no| to customize the mode of the path created on nfs server. If not provided, the server's default mask will be used|
|modeType|e.g `recursive` or `non-recursive`|no| This tells the driver if the chmod should be done recursively or not. Only works when `mode` is specified. Default to `non-recursive`|
|allowShared| `"true"` or `"false"`|no | default to `"false`. If set, the data of all PVs with the same path will be shared|

### To use dynamic provisioned PV

please add the following definition to the StorageClass yaml file:
```yaml
provisioner: nas.csi.cds.net
parameters:
  volumeAs: subpath
  servers: "140.210.75.194/nfsshare/nas-csi-cds-pv, 140.210.75.195/nfsshare/nas-csi-cds-pv"
  server: "140.210.75.195"
  path: "/nfsshare"
  vers: "4.0"
  strategy: "RoundRobin" 
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
|servers|"140.210.75.194/nfsshare/nas-csi-cds-pv, 140.210.75.195/nfsshare/nas-csi-cds-pv"|no|multi servers are supported by a StorageClass. It should be separated by "," between each server/path.|
|server|e.g. "140.210.75.195"|no|the mount point of the nfs server,can be found on NAS product list|
|strategy|e.g. "RoundRobin"|no| Only used for multi servers option. The value is supposed to be "RoundRobin" or "Random". The default value is "RoundRobin"|
|path|e.g. "/nfsshare"|no|the root path that the pv will dynamically provisioned on the NAS server. It must start with `/nfsshare`. |
|vers|`3` or `4.0`|no| the protocol version to use for the nfs mount. if not provided, `4.0` will be used by default
|options|e.g. `noresvport`|no| options for the nfs mount. If not provided it will be set to `noresvport` for `vers=4.0` and `noresvport,nolock,tcp` for `vers=3`|
|mode|e.g. `0777`|no| to customize the mode of the path created on nfs server. If not provided, the server's default mask will be used
|modeType|e.g `recursive` or `non-recursive`|no| This tells the driver if the chmod should be done recursively or not. Only works when `mode` is specified. Default to `non-recursive`
|archiveOnDelete|`"true"` or `"false"`|no| only works when the `reclaim police` is `delete`. if `true`, the content of the pv will be archived on nfs instead of be deleted. Default to `false`|

Kindly Remind: 

​	a) server and path are as a whole to use.

​	b) servers and server cant be empty together. It means that servers or server is not empty at least in one yaml. 

## To use the OSS driver

Examples can be found here [here](!https://github.com/capitalonline/cds-csi-driver/tree/master/example/oss)

### To use static provisioned PV (only support)

PersistentVolume yaml file:

```yaml
apiVersion: v1
kind: PersistentVolume
metadata:
  name: oss-csi-pv
  labels:
    name: oss-csi-pv
spec:
  capacity:
    storage: 5Gi
  accessModes:
    - ReadWriteOnce
  persistentVolumeReclaimPolicy: Retain
  csi:
    driver: oss.csi.cds.net
    # set volumeHandle same value pv name
    volumeHandle: oss-csi-pv
    volumeAttributes:
      bucket: "***"
      url: "http://oss-cnbj01.cdsgss.com"
      akId: "***"
      akSecret: "***"
      path: "***"
```

Description:

`driver`: should always be `oss.csi.cds.net`

`volumeHandle`: should be same with pv name

`volumeAttributes`:

​	`bucket`: register in CDS and get one unique name

​	`url`: should always be `http://oss-cnbj01.cdsgss.com`

​	`akId`: get it from your own bucket in CDS

​	`akSecret`: get it from your own bucket in CDS

​	`path`: created in your bucket 

***Note***: path default value is `/` if input empty