#!/usr/bin/env bash

#!/usr/bin/env bash
set -e

BUCKET="csi-testing-sjz"
URL="http://oss-cnbj01.cdsgss.com"
AKID="9200dc818c385ccf968e0a5d84abe458"
AKS="19459552ac785d0483bdaf4d121d21e4"
NAMESPACE="csi-test"

function cleanup(){
    echo "=> cleaning up assets"
    kubectl -n ${NAMESPACE} delete pod --all || true
    kubectl -n ${NAMESPACE} delete pvc --all || true
    kubectl delete pv --all || true
    echo "=> Done!"
}

function case1(){
    echo "=> Case 1: test case with static pv and path is exist in remote bucket"
    echo "=> Case 1: let's create pod1 with persistentVolumeReclaimPolicy: Retain"
    cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: pod1
  namespace: ${NAMESPACE}
spec:
  nodeName: worker001
  containers:
    - name: "hello-world"
      image: "tutum/hello-world"
      volumeMounts:
        - name: pvc-oss
          mountPath: "/data"
  volumes:
    - name: pvc-oss
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
    storage: 2Gi
  accessModes:
    - ReadWriteMany
  persistentVolumeReclaimPolicy: Retain
  csi:
    driver: oss.csi.cds.net
    # set volumeHandle same value pv name
    volumeHandle: pv1
    volumeAttributes:
      bucket: ${BUCKET}
      url: ${URL}
      akId: ${AKID}
      akSecret: ${AKS}
      path: "/data"
EOF
    kubectl -n ${NAMESPACE} wait --for condition=Ready pod/pod1 --timeout 30s

    echo "=> Case 1: test if pod1 is created"
    kubectl -n ${NAMESPACE} get pod pod1
    echo "=> Done!"

    echo "=> Case 1: test if pvc1 is created"
    kubectl -n ${NAMESPACE} get pvc pvc1
    echo "=> Done!"

    echo "=> Case 1: test if pv1 is created"
    kubectl -n ${NAMESPACE} get pv pv1
    echo "=> Done!"

    echo "=> Case 1: test if bucket is mounted to pod1, please check it manually"
    ansible worker001 -m shell -a "df -h | grep s3fs"
    echo "=> Done!"

    echo "=> Case 1: let's remove pod1 as well as pvc1"
    kubectl -n ${NAMESPACE} delete pod pod1
    kubectl -n ${NAMESPACE} delete pvc pvc1
    echo "=> Done"

    echo "=> let's delete pv1"
    kubectl delete pv pv1
    echo "=> Done"

    echo "=> Case 1: test if bucket is unmounted from pod1, please check it manually"
    ansible worker001 -m shell -a "df -h | grep s3fs"
    echo "=> Done!"

    echo "Case 1: Passed"
    cleanup
}

function case2(){
    echo "=> Case 1: test case with static pv and path is not exist in remote bucket"
    echo "=> Case 1: let's create pod1 with persistentVolumeReclaimPolicy: Retain"
    cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: pod1
  namespace: ${NAMESPACE}
spec:
  nodeName: worker001
  containers:
    - name: "hello-world"
      image: "tutum/hello-world"
      volumeMounts:
        - name: pvc-oss
          mountPath: "/data"
  volumes:
    - name: pvc-oss
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
    storage: 2Gi
  accessModes:
    - ReadWriteMany
  persistentVolumeReclaimPolicy: Retain
  csi:
    driver: oss.csi.cds.net
    # set volumeHandle same value pv name
    volumeHandle: pv1
    volumeAttributes:
      bucket: ${BUCKET}
      url: ${URL}
      akId: ${AKID}
      akSecret: ${AKS}
      path: "/tmp"
EOF
    kubectl -n ${NAMESPACE} wait --for condition=Ready pod/pod1 --timeout 30s

    echo "=> Case 1: test if pod1 is created"
    kubectl -n ${NAMESPACE} get pod pod1
    echo "=> Done!"

    echo "=> Case 1: test if pvc1 is created"
    kubectl -n ${NAMESPACE} get pvc pvc1
    echo "=> Done!"

    echo "=> Case 1: test if pv1 is created"
    kubectl -n ${NAMESPACE} get pv pv1
    echo "=> Done!"

    echo "=> Case 1: test if pod is not running and log shows that remote bucket path is not exist"
    kubectl describe pods pod1 -n ${NAMESPACE} | grep Remote
    echo "=> Done!"

    echo "=> Case 1: let's remove pod1 as well as pvc1"
    kubectl -n ${NAMESPACE} delete pod pod1
    kubectl -n ${NAMESPACE} delete pvc pvc1
    echo "=> Done"

    echo "=> let's delete pv1"
    kubectl delete pv pv1
    echo "=> Done"

    echo "Case 1: Passed"
    cleanup
}

case1
case2

