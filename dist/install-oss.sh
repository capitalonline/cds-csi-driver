#! /bin/sh

cp -rf /oss-server /host/config/bin/oss-server
cp -rf /oss.service /host/config/system/oss.service

nsenter -m -u -i -p -F -t 1 sh -c "systemctl enable oss.service"
nsenter -m -u -i -p -F -t 1 sh -c "systemctl start oss.service"
