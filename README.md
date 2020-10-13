# cds-csi-driver 

A Container Storage Interface (CSI) Driver for Capitalonline(aka. CDS) cloud. This CSI plugin allows you to use CDS storage with the kubernetes cluster hosted or managed by CDS.

It currently supports CDS File Storage(NAS) and CDS Object Storage Service(OSS).
The support for the Block Storage will be added soon.

## To deploy

To deploy the CSI NAS driver to your k8s, simply run:
```bash
kubectl create -f https://raw.githubusercontent.com/capitalonline/cds-csi-driver/master/deploy/nas/deploy.yaml
```

To deploy the CSI OSS driver to your k8s, simply run:

```bash
kubectl create -f https://raw.githubusercontent.com/capitalonline/cds-csi-driver/master/deploy/oss/deploy.yaml
```

To deploy the CSI Block driver to your k8s, simply run:

```bash
kubectl create -f https://raw.githubusercontent.com/capitalonline/cds-csi-driver/master/deploy/block/deploy.yaml
```

## To run tests

**NAS:**

1. Make sure that you have a Kubernetes cluster accessible with `kubectl`
2. Provision a nfs server with exports `/nfsshare *(insecure,rw,async,no_root_squash,fsid=1000)`
3. Run `make test-prerequisite` to build the image and deploy the driver to your k8s cluster
4. Run `sudo NFS=<your nfs server ip> make test`

**OSS:**

1. Make sure that you have a Kubernetes cluster accessible with `kubectl`
2. Register a Storage Service in CDS 
3. Run `make test-prerequisite` to build the image and deploy the driver to your k8s cluster
4. Run `make oss-test`

**Block：**

1. Make sure that you have a Kubernetes cluster accessible with `kubectl`
2. Run `make test-prerequisite` to build the image and deploy the driver to your k8s cluster
3. Run `make block-test`

## To use the NAS driver
Examples can be found [here](!https://github.com/capitalonline/cds-csi-driver/tree/master/example/nas)

### Static PV

pv.yaml

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

|     Key      | Value                              | Required | Description                                                  |
| :----------: | :--------------------------------- | :------: | :----------------------------------------------------------- |
|    driver    | nas.csi.cds.net                    |   yes    | CDS csi driver's name                                        |
| volumeHandle | <pv_name>                          |   yes    | Should be same with pv name                                  |
|    server    | e.g. <br />140.210.75.195          |   yes    | the mount point of the nfs server,can be found on NAS product list |
|     path     | e.g.<br />/nfsshare/division1      |   yes    | the path that the pv will use on the nfs server. It must start with `/nfsshare`. Each pv will use a separate directory(e.g `/nfsshare/division1/volumeHandleValue"`when `allowShared` is `false`. Otherwsise, the path of pv will be the same to `path`, which means all PVs with the same path will share data |
|     vers     | `3` or `4.0`                       |    no    | the protocol version to use for the nfs mount. if not provided, `4.0` will be used by default |
|   options    | e.g. `noresvport`                  |    no    | options for the nfs mount. If not provided it will be set to `noresvport` for `vers=4.0` and `noresvport,nolock,tcp` for `vers=3` |
|     mode     | e.g. `0777`                        |    no    | to customize the mode of the path created on nfs server. If not provided, the server's default mask will be used |
|   modeType   | e.g `recursive` or `non-recursive` |    no    | This tells the driver if the chmod should be done recursively or not. Only works when `mode` is specified. Default to `non-recursive` |
| allowShared  | `"true"` or `"false"`              |    no    | default to `"false`. If set, the data of all PVs with the same path will be shared |

### Dynamic PV

#### volumeAs: subpath 

sc.yaml

```yaml
provisioner: nas.csi.cds.net
reclaimPolicy: Delete
parameters:
  volumeAs: subpath
  servers: 140.210.75.194/nfsshare/nas-csi-cds-pv, 140.210.75.195/nfsshare/nas-csi-cds-pv
  server: 140.210.75.195
  path: /nfsshare
  vers: 4.0
  strategy: RoundRobin
  options: noresvport
  archiveOnDelete: true
```

Description:

|       Key       | Value                                                        | Required | Description                                                  |
| :-------------: | :----------------------------------------------------------- | :------: | :----------------------------------------------------------- |
|   provisioner   | nas.csi.cds.net                                              |   yes    | CDS csi driver's name                                        |
|  reclaimPolicy  | Delete \| Retain                                             |   yes    | `Delete` means that PV will be deleted with PVC delete<br />`Retain` means that PV will be retained when PVC delete |
|    volumeAs     | subpath                                                      |    no    | Default is `subpath`<br />`subpath`  means each PV will be provisioned on a specified NAS server.<br />`filesystem` means each PV will be provision with a new NAS Storage and use the storage exclusively. |
|     servers     | eg.<br />140.210.75.194/nfsshare/nas-csi-cds-pv, 140.210.75.195/nfsshare/nas-csi-cds-pv |    no    | multi servers are supported by a StorageClass. It should be separated by "," between each server/path. |
|     server      | e.g. <br />140.210.75.195                                    |    no    | the mount point of the nfs server,can be found on NAS product list |
|    strategy     | e.g. <br />RoundRobin                                        |    no    | Only used for multi servers option. The value is supposed to be "RoundRobin" or "Random". The default value is "RoundRobin" |
|      path       | e.g. <br />/nfsshare                                         |    no    | the root path that the pv will dynamically provisioned on the NAS server. It must start with `/nfsshare`. |
|      vers       | `3` or `4.0`                                                 |    no    | the protocol version to use for the nfs mount. if not provided, `4.0` will be used by default |
|     options     | e.g. `noresvport`                                            |    no    | options for the nfs mount. If not provided it will be set to `noresvport` for `vers=4.0` and `noresvport,nolock,tcp` for `vers=3` |
|      mode       | e.g. `0777`                                                  |    no    | to customize the mode of the path created on nfs server. If not provided, the server's default mask will be used |
|    modeType     | e.g `recursive` or `non-recursive`                           |    no    | This tells the driver if the chmod should be done recursively or not. Only works when `mode` is specified. Default to `non-recursive` |
| archiveOnDelete | `"true"` or `"false"`                                        |    no    | only works when the `reclaim police` is `delete`. if `true`, the content of the pv will be archived on nfs instead of be deleted. Default to `false` |

Kindly Remind: 

​	a) server and path are as a whole to use.

​	b) servers and server cant be empty together. It means that servers or server is not empty at least in one yaml. 

#### volumeAs: filesystem 

sc.yaml

```yaml
provisioner: nas.csi.cds.net
reclaimPolicy: Delete
parameters:
  volumeAs: filesystem
  protocolType: NFS
  storageType: high_disk
  siteID: *** 
  clusterID: *** 
  deleteVolume: true 
```

Description:

| Key           | Value            | Required | Description                                                  |
| ------------- | ---------------- | -------- | ------------------------------------------------------------ |
| provisioner   | nas.csi.cds.net  | yes      | CDS csi driver's name                                        |
| reclaimPolicy | Delete \| Retain | yes      | `Delete` means that PV will be deleted with PVC delete<br />`Retain` means that PV will be retained when PVC delete |
| volumeAs      | filesystem       | yes      | Default is `subpath`<br />`subpath`  means each pv will be provisioned on a specified NAS server.<br />`filesystem` means each PV will be provision with a new NAS Storage and use the storage exclusively. |
| storageType   | high_disk        | yes      | Must be 'high_disk', only support 'high_disk'                |
| siteID        | ***              | yes      | Cluster's site_id                                            |
| clusterID     | ***              | yes      | cluster_id                                                   |
| deleteVolume  | true \| false    | no       | Optional, default is 'false'<br />`false` means that NAS storage will retain when PV delete<br />`true` means that NAS storage will be deleted with PV delete |
| protocolType  | NFS              | no       | Optional, default is 'NFS'<br />must be 'NFS', only support 'NFS' |

## To use the OSS driver

Examples can be found [here](!https://github.com/capitalonline/cds-csi-driver/tree/master/example/oss)

### Static PV 

pv.yaml

```yaml
spec:
  csi:
    driver: oss.csi.cds.net
    # set volumeHandle same value pv name
    volumeHandle: <name of the pv>
    volumeAttributes:
      bucket: "***"
      url: "http://oss-cnbj01.cdsgss.com"
      akId: "***"
      akSecret: "***"
      path: "***"
```

Description:

| Key          | Value                          | Required | Description                              |
| ------------ | ------------------------------ | -------- | ---------------------------------------- |
| driver       | oss.csi.cds.net                | yes      | CDS csi driver's name                    |
| volumeHandle | <name of the pv>               | yes      | Should be same with pv name              |
| bucket       | <bucket name>                  | yes      | Register in CDS and get one unique name  |
| url          | `http://oss-cnbj01.cdsgss.com` | yes      | Should be `http://oss-cnbj01.cdsgss.com` |
| akId         | <access_key>                   | yes      | Get it from your own bucket's in CDS web |
| akSecret     | <acckedd_key_secret>           | yes      | Get it from your own bucket's in CDS web |
| path         | <bucket_path>                  | yes      | Bucket path, default is `/`              |



## To use the Disk driver 

### Dynamic PV

sc.yaml

```yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: disk-csi-cds-sc
parameters:
  fsType: "ext4"			
  storageType: "high_disk"
  iops: "3000"
  siteId: "beijing001"
  zoneId: "WuxiA-POD10-CLU02"
provisioner: disk.csi.cds.net
reclaimPolicy: Retain
volumeBindingMode: WaitForFirstConsumer    
```

Description:

| Key               | Value                     | Required | Description                                                  |
| ----------------- | ------------------------- | -------- | ------------------------------------------------------------ |
| fsType            | [ xfs\|ext4\|ext3 ]       | yes      | Linux filesystem type                                        |
| storageType       | [ high_disk\|ssd_disk ]   | yes      | Only support `high_disk` and `ssd_disk`.<br />`high_disk` shoud be with iops `3000`.<br />`ssd_disk` should be with iops `[5000|7500|10000]`. |
| iops              | [3000\|5000\|7500\|10000] | yes      | Only support `3000` `5000` `7500` and `1000`.<br />Should combined with `storageType`. |
| siteId            | Eg. "Beijing001"          | yes      | Cluster's site_id.                                           |
| zoneId            | Eg. "Beijing-POD10-CLU02" | yes      | Cluster's id.                                                |
| provisioner       | disk.csi.cds.net          | yes      | Disk driver which installed default.                         |
| reclaimPolicy     | [ Delete\|Retain ]        | yes      | `Delete` means that PV will be deleted with PVC delete<br/>`Retain` means that PV will be retained when PVC delete |
| volumeBindingMode | WaitForFirstConsumer      | yes      | Only suport `WaitForFirstConsumer` pollicy for disk.csi.cds.net driver. |

Kindly Remind: 

​	For disk storage, recommending using `volumeBindingMode:` `WaitForFirstConsumer ` in SC.yaml. 

​	If not, please apply your `disk.csi.cds.net` csi driver's `csi-provisioner` in k8s with following:

```yaml
- args: 
    - "--csi-address=$(ADDRESS)"
    - "--v=5"
    - "--feature-gates=Topology=true"			# 添加这个参数，开启 Topology 
  env: 
    - 
      name: ADDRESS
      value: /socketDir/csi.sock
  image: "registry-bj.capitalonline.net/cck/csi-provisioner:v1.5.0"
  name: csi-provisioner
  volumeMounts: 
    - 
      mountPath: /socketDir
      name: socket-dir
```

and then refer the following SC.yaml

```yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: disk
parameters:
  fstype: "ext4" 
  storageType: "high_disk"
  iops: "3000"
  siteId: "ca0bd848-9b59-40a2-9f57-d64fbc72a9df"
provisioner: disk.csi.cds.net
reclaimPolicy: Delete
volumeBindingMode: Immediate    # Immediate policy in SC 
allowedTopologies:				# using allowedTopologies in sc 
- matchLabelExpressions:
  - key: topology.kubernetes.io/zone	
    values:
    - WuxiA-POD10-CLU02
```



