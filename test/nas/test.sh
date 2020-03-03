#!/usr/bin/env bash
set -e

NFS=${NFS}
ROOT_DIR="test_tmp"
MOUNT_DIR="${ROOT_DIR}/mnt"
SHARE_DIR="/nfsshare"
NAMESPACE="csi-test"

function init() {
    if [[ -z "$1" ]]; then
        echo "=> No nfs server is provided. Try again with a defined env var NFS"
        echo "=> Fox example:  NFS=<ip> make test"
        exit 1
    fi

    echo "=> Test if NFS server is connectible: $1"
    if ! showmount -e $1; then
        echo "=> cannot connect to $1"
        exit 1
    fi

    echo "=> Setting up environment"
    umount -f ${MOUNT_DIR} || true
    rm -rf ${MOUNT_DIR}
    mkdir -p ${MOUNT_DIR}
    mount -v -t nfs -o "vers=4,noresvport" ${NFS}:${SHARE_DIR} ${MOUNT_DIR}
    rm -rf ${MOUNT_DIR}/* || true
    kubectl create namespace ${NAMESPACE} >/dev/null 2>&1 || true
    kubectl delete -n ${NAMESPACE} pod --all || true
    kubectl delete -n ${NAMESPACE} pvc --all || true
    kubectl delete pv --all || true
    kubectl delete sc --all || true

    echo "=> End of the initialization"
}

function case1(){
    echo "=> Case 1: static pv"
    echo "=> Case 1: let's create pod1 with persistentVolumeReclaimPolicy: Retain"
    cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: pod1
  namespace: ${NAMESPACE}
spec:
  containers:
    - name: "hello-world"
      image: "tutum/hello-world"
      volumeMounts:
        - name: pvc-nas
          mountPath: "/data"
  volumes:
    - name: pvc-nas
      persistentVolumeClaim:
        claimName: pvc1
---
kind: PersistentVolumeClaim
apiVersion: v1
metadata:
  name: pvc1
  namespace: ${NAMESPACE}
spec:
  accessModes:
    - ReadWriteMany
  resources:
    requests:
      storage: 2Gi
---
apiVersion: v1
kind: PersistentVolume
metadata:
  name: pv1
spec:
  capacity:
    storage: 5Gi
  accessModes:
    - ReadWriteMany
  persistentVolumeReclaimPolicy: Retain
  csi:
    driver: nas.csi.cds.net
    # volumeHandle set same value as pvname
    volumeHandle: pv1
    volumeAttributes:
      server: "${NFS}"
      path: "${SHARE_DIR}/pv1"
      vers: "4.0"
EOF
    kubectl wait -n ${NAMESPACE} --for condition=Ready pod/pod1 --timeout 30s

    echo "=> Case 1: test if pod1 is created"
    kubectl -n csi-test get pod pod1
    echo "=> Done!"

    echo "=> Case 1: test if the directory for pod1 is created on nfs"
    if [[ ! -d "$MOUNT_DIR"/pv1 ]]; then
        echo "=> $MOUNT_DIR/pv1 is not on the nfs server"
        exit 1
    fi
    echo "=> Done!"

    echo "=> Case 1: let's create pod2 with persistentVolumeReclaimPolicy: Delete"
    cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: pod2
  namespace: ${NAMESPACE}
spec:
  containers:
    - name: "hello-world"
      image: "tutum/hello-world"
      volumeMounts:
        - name: pvc-nas
          mountPath: "/data"
  volumes:
    - name: pvc-nas
      persistentVolumeClaim:
        claimName: pvc2
---
kind: PersistentVolumeClaim
apiVersion: v1
metadata:
  name: pvc2
  namespace: ${NAMESPACE}
spec:
  accessModes:
    - ReadWriteMany
  resources:
    requests:
      storage: 2Gi
---
apiVersion: v1
kind: PersistentVolume
metadata:
  name: pv2
spec:
  capacity:
    storage: 5Gi
  accessModes:
    - ReadWriteMany
  persistentVolumeReclaimPolicy: Delete
  csi:
    driver: nas.csi.cds.net
    # volumeHandle set same value as pvname
    volumeHandle: pv2
    volumeAttributes:
      server: "${NFS}"
      path: "${SHARE_DIR}/pv2"
      vers: "4.0"
EOF
    kubectl wait -n ${NAMESPACE} --for condition=Ready pod/pod2 --timeout 30s

    echo "=> Case 1: test if pod2 is created"
    kubectl -n csi-test get pod pod2
    echo "=> Done!"

    echo "=> Case 1: test if the directory for pod2 is created on nfs"
    if [[ ! -d "$MOUNT_DIR"/pv2 ]]; then
        echo "=> $MOUNT_DIR/pv2 is not on the nfs server"
        exit 1
    fi

    echo "=> Case 1: test if both pv1 and pv2 exists in the k8s"
    kubectl -n csi-test get pv pv1
    kubectl -n csi-test get pv pv2
    echo "=> Done!"

    echo "=> Case 1: let's remove pod1, pod2 as well as pvc1, pvc2"
    kubectl -n csi-test delete pod pod1
    kubectl -n csi-test delete pod pod2
    kubectl -n csi-test delete pvc pvc1
    kubectl -n csi-test delete pvc pvc2
    echo "=> Done"

    echo "=> Case 1: test if pv1's RECLAIM POLICY is Retain and the STATUS is Released"
    kubectl get pv pv1 | grep 'Retain.*Released'
    echo "=> Done"

    echo "=> Case 1: test if pv2's RECLAIM POLICY is Delete and the STATUS is Failed"
    kubectl get pv pv2 | grep 'Delete.*Failed'
    echo "=> Done"

    echo "=> let's delete both pv1 and pv2"
    kubectl delete pv pv1 pv2
    echo "=> Done"

    echo "Case 1: Passed"
}


init ${NFS}
case1