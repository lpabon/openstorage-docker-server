#!/bin/bash
set -x

sudo docker volume create -d osd-gateway > id
sudo docker volume inspect `cat id`
sudo docker -D volume rm `cat id`

