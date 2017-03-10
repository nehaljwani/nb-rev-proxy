#!/bin/bash
if [ -z ${GOPATH} ]
then
  echo "A cute gopher died because you didn't set GOPATH :("
  exit -1
fi
go get -d -v
go build -o nbrp nbrp.go
mkdir -p /tmp/datadir
docker-compose build
docker-compose up -d
docker-compose scale web=3
# docker-compose stop
