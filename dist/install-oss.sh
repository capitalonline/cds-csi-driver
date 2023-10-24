#! /bin/sh

cp -f /oss-server /srv/oss-server
cp -f /oss.service /host/config/system/oss.service

code=nsenter -m -u -p -F -t 1 sh -c "(which s3fs || ((which apt && apt install s3fs -y) || (which yum && yum install s3fs -y))) && systemctl daemon-reload && systemctl enable oss.service && systemctl start oss.service"
if [ $? -eq 0 ]; then
    exit 0
fi

echo "failed to install s3fs"
exit -1
