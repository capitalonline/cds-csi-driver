# cds-csi-driver

## deploy

to deploy NAS CSI driver, run:
```
kubectl apply -f https://raw.githubusercontent.com/capitalonline/cds-csi-driver/master/deploy/nas/deploy.yaml
```

## Test

1. Make sure that you have a Kubernetes cluster accessible with `kubectl`
2. Provision a nfs server with exports `/nfsshare *(insecure,rw,async,no_root_squash,fsid=1000)`
3. Run `make test-prerequisite` to build the image and deploy the driver to your k8s cluster
4. Run `NFS=<your nfs server ip> make test`
