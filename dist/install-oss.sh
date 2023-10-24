#! /bin/sh

cp -f /oss-server /srv/oss-server
cp -f /oss.service /host/config/system/oss.service

code=nsenter -m -u -p -F -t 1 sh -c "stemctl daemon-reload; systemctl enable oss.service; systemctl start oss.service"
if [ $? -eq 0 ]; then
    echo "111111"
fi
echo "2222222"

exit -1
