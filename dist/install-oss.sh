#! /bin/sh

cp -rf /oss-server /host/config/bin/
cp -rf /oss.service /host/config/system/

nsenter -m -u -i -p -F -t 1 sh -c "systemctl enable oss.service"
nsenter -m -u -i -p -F -t 1 sh -c "systemctl start oss.service"
