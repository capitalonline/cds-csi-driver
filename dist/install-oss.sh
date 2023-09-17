#! /bin/sh

cp -f /oss-server /srv/oss-server
cp -f /oss.service /host/config/system/oss.service

nsenter -m -u -p -F -t 1 sh -c "systemctl daemon-reload; systemctl enable oss.service; systemctl start oss.service"
