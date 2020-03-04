#!/usr/bin/env bash
set -e

NFS=${NFS}
ROOT_DIR="test_tmp"
MOUNT_DIR="${ROOT_DIR}/mnt"
SHARE_DIR="/nfsshare"
NAMESPACE="csi-test"

function cleanup(){
    echo "=> cleaning up assets"
    kubectl -n ${NAMESPACE} delete pod --all || true
    kubectl -n ${NAMESPACE} delete pvc --all || true
    kubectl delete pv --all || true
    kubectl delete sc --all || true
    rm -rf ${MOUNT_DIR}/* || true
    echo "=> Done!"
}

function init() {
    if [[ -z "$1" ]]; then
        echo "=> No nfs server is provided. Try again with a defined env var NFS"
        echo "=> Fox example:  NFS=<ip> make test"
        exit 1
    fi

    echo "=> Test if NFS server is connectible: $1"
    if ! showmount -e $1; then
        echo "=> Failed: cannot connect to $1"
        exit 1
    fi

    echo "=> Setting up environment"
    umount -f ${MOUNT_DIR} || true
    rm -rf ${MOUNT_DIR}
    mkdir -p ${MOUNT_DIR}
    mount -v -t nfs -o "vers=4,noresvport" ${NFS}:${SHARE_DIR} ${MOUNT_DIR}
    kubectl create namespace ${NAMESPACE} >/dev/null 2>&1 || true
    echo "=> End of the initialization"
    cleanup
}

function case1(){
    echo "=> Case 1: test case with static pv"
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
    kubectl -n ${NAMESPACE} wait --for condition=Ready pod/pod1 --timeout 30s

    echo "=> Case 1: test if pod1 is created"
    kubectl -n ${NAMESPACE} get pod pod1
    echo "=> Done!"

    echo "=> Case 1: test if the directory for pod1 is created on nfs"
    if [[ ! -d "${MOUNT_DIR}"/pv1 ]]; then
        echo "=> Failed: ${MOUNT_DIR}/pv1 is not on the nfs server"
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
    kubectl -n ${NAMESPACE} get pod pod2
    echo "=> Done!"

    echo "=> Case 1: test if the directory for pod2 is created on nfs"
    if [[ ! -d "${MOUNT_DIR}"/pv2 ]]; then
        echo "=> Failed: ${MOUNT_DIR}/pv2 is not on the nfs server"
        exit 1
    fi

    echo "=> Case 1: test if both pv1 and pv2 exists in the k8s"
    kubectl -n ${NAMESPACE} get pv pv1
    kubectl -n ${NAMESPACE} get pv pv2
    echo "=> Done!"

    echo "=> Case 1: let's remove pod1, pod2 as well as pvc1, pvc2"
    kubectl -n ${NAMESPACE} delete pod pod1
    kubectl -n ${NAMESPACE} delete pod pod2
    kubectl -n ${NAMESPACE} delete pvc pvc1
    kubectl -n ${NAMESPACE} delete pvc pvc2
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
    cleanup
}

function case2(){
    echo "=> Case 2: test case with dynamic pv"
    echo "=> Case 2: let's create 3 storage class with different archiveOnDelete on and off"
    cat <<EOF | kubectl apply -f -
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: sc1
parameters:
  volumeAs: subpath
  server: "${NFS}"
  path: "${SHARE_DIR}"
  vers: "4.0"
  options: noresvport
  archiveOnDelete: "true"
provisioner: nas.csi.cds.net
reclaimPolicy: Delete
---
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: sc2
parameters:
  volumeAs: subpath
  server: "${NFS}"
  path: "${SHARE_DIR}"
  vers: "4.0"
  options: noresvport
  archiveOnDelete: "false"
provisioner: nas.csi.cds.net
reclaimPolicy: Delete
---
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: sc3
parameters:
  volumeAs: subpath
  server: "${NFS}"
  path: "${SHARE_DIR}"
  vers: "4.0"
  options: noresvport
  archiveOnDelete: "false"
provisioner: nas.csi.cds.net
reclaimPolicy: Retain
EOF
    echo "=> Done!"

    echo "=> Case 2: let's create pvc3, pvc4 and pvc5 using the above storage class separately"
    cat <<EOF | kubectl apply -f -
kind: PersistentVolumeClaim
apiVersion: v1
metadata:
  namespace: ${NAMESPACE}
  name: pvc3
spec:
  accessModes:
    - ReadWriteMany
  storageClassName: sc1
  resources:
    requests:
      storage: 1Gi
---
kind: PersistentVolumeClaim
apiVersion: v1
metadata:
  namespace: ${NAMESPACE}
  name: pvc4
spec:
  accessModes:
    - ReadWriteMany
  storageClassName: sc2
  resources:
    requests:
      storage: 1Gi
---
kind: PersistentVolumeClaim
apiVersion: v1
metadata:
  namespace: ${NAMESPACE}
  name: pvc5
spec:
  accessModes:
    - ReadWriteMany
  storageClassName: sc3
  resources:
    requests:
      storage: 1Gi
EOF
    echo "=> Done!"

    echo "=> Case 2: let's create pod3, pod4 and pod5 using the pvc created above"
    cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  namespace: ${NAMESPACE}
  name: pod3
spec:
  containers:
    - name: "hello-world"
      image: "tutum/hello-world"
      volumeMounts:
        - name: pvc-nas-sc
          mountPath: "/data"
  volumes:
    - name: pvc-nas-sc
      persistentVolumeClaim:
        claimName: pvc3
---
apiVersion: v1
kind: Pod
metadata:
  namespace: ${NAMESPACE}
  name: pod4
spec:
  containers:
    - name: "hello-world"
      image: "tutum/hello-world"
      volumeMounts:
        - name: pvc-nas-sc
          mountPath: "/data"
  volumes:
    - name: pvc-nas-sc
      persistentVolumeClaim:
        claimName: pvc4
---
apiVersion: v1
kind: Pod
metadata:
  namespace: ${NAMESPACE}
  name: pod5
spec:
  containers:
    - name: "hello-world"
      image: "tutum/hello-world"
      volumeMounts:
        - name: pvc-nas-sc
          mountPath: "/data"
  volumes:
    - name: pvc-nas-sc
      persistentVolumeClaim:
        claimName: pvc5
EOF
    kubectl -n ${NAMESPACE} wait --for condition=Ready pod/pod3 pod/pod4 pod/pod5 --timeout 30s
    echo "=> Done!"

    echo "=> Case 2: verify if 3 pods are all created"
    kubectl -n ${NAMESPACE} get pod pod3
    kubectl -n ${NAMESPACE} get pod pod4
    kubectl -n ${NAMESPACE} get pod pod5
    echo "=> Done!"

    echo "=> Case 2: verify if 3 pvcs are created"
    out3=$(kubectl -n ${NAMESPACE} get pvc pvc3)
    out4=$(kubectl -n ${NAMESPACE} get pvc pvc4)
    out5=$(kubectl -n ${NAMESPACE} get pvc pvc5)
    echo ${out3} | grep -iF Bound
    echo ${out4} | grep -iF Bound
    echo ${out5} | grep -iF Bound
    echo "=> Done!"

    echo "=> Case 2: verify if 3 pvs are created correctly"
    pv3_name=$(kubectl -n ${NAMESPACE} get pvc pvc3 | grep -iF Bound | tr -s ' '| cut -d ' ' -f 3)
    pv4_name=$(kubectl -n ${NAMESPACE} get pvc pvc4 | grep -iF Bound | tr -s ' '| cut -d ' ' -f 3)
    pv5_name=$(kubectl -n ${NAMESPACE} get pvc pvc5 | grep -iF Bound | tr -s ' '| cut -d ' ' -f 3)
    kubectl -n ${NAMESPACE} get pv ${pv3_name} | grep 'Delete.*Bound'
    kubectl -n ${NAMESPACE} get pv ${pv4_name} | grep 'Delete.*Bound'
    kubectl -n ${NAMESPACE} get pv ${pv5_name} | grep 'Retain.*Bound'
    echo "=> Done!"

    echo "=> Case 2: verify if directories is created on the nfs server"
    if [[ ! -d "${MOUNT_DIR}"/${pv3_name} ]]; then
        echo "=> Failed: ${MOUNT_DIR}/${pv3_name} is not on the nfs server"
        exit 1
    fi
    if [[ ! -d "${MOUNT_DIR}"/${pv4_name} ]]; then
        echo "=> Failed: ${MOUNT_DIR}/${pv4_name} is not on the nfs server"
        exit 1
    fi
    if [[ ! -d "${MOUNT_DIR}"/${pv5_name} ]]; then
        echo "=> Failed: ${MOUNT_DIR}/${pv5_name} is not on the nfs server"
        exit 1
    fi
    echo "=> Done!"

    echo "=> Case 2: let's remove all the pods and pvc"
    kubectl -n ${NAMESPACE} delete pod pod3
    kubectl -n ${NAMESPACE} delete pod pod4
    kubectl -n ${NAMESPACE} delete pod pod5
    kubectl -n ${NAMESPACE} delete pvc pvc3
    kubectl -n ${NAMESPACE} delete pvc pvc4
    kubectl -n ${NAMESPACE} delete pvc pvc5
    echo "=> Done!"

    echo "=> Case 2: verify if pvs are handled correctly"
    kubectl get pv ${pv3_name} || true
    kubectl get pv ${pv4_name} || true
    kubectl get pv ${pv5_name} | grep -iF "Retain"
    echo "=> Done!"

    echo "=> Case 2: verify is nfs server handles archiveOnDelete correctly"
    if ! ls ${MOUNT_DIR} | grep "archived-${pv3_name}" ; then
        echo "=> Failed: pv ${pv3_name} for pod4 should be archived:"
        exit 1
    fi
    if [[ -d ${MOUNT_DIR}/${pv4_name} ]]; then
        echo "=> Failed: pv ${pv4_name} for pod4 should be removed from nfs server"
        exit 1
    fi
    if [[ ! -d ${MOUNT_DIR}/${pv5_name} ]]; then
        echo "=> Failed: pv ${pv5_name} for pod5 should not be removed"
        exit 1
    fi
    echo "=> Done!"

    echo "Case 2: Passed"
    #cleanup
}

init ${NFS}
case1
case2


