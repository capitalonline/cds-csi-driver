#!/usr/bin/env bash
set -e

echo -e "****************************\n** Start Testing NAS Driver \n****************************\n"
test/nas/test.sh
echo -e "****************************\n** Nas Driver Test Passed \n****************************\n"
echo
echo -e "****************************\n** Start Testing OSS Driver \n****************************\n"
test/oss/test.sh
echo -e "****************************\n** OSS Driver Test Passed \n****************************\n"