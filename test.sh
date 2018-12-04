#!/bin/bash
set -x

sudo docker volume create -d osd-gateway -o size=12354 > vname
sudo docker -D volume rm `cat vname`

