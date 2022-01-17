#!/usr/bin/env bash


OUTNAME="collector"
PACKAGE="main"
VERSION="2.3"
BUILDTIME=$(date '+%Y-%m-%dT%H:%M:%S')
GIT_COMMIT=$(git rev-list -1 HEAD)
GIT_COMMENT=$(git show -s --format=%s)

LDFLAGS=(
  "-X '${PACKAGE}.version=${VERSION}'"
  "-X '${PACKAGE}.BuildTime=${BUILDTIME}'"
  "-X '${PACKAGE}.GitCommit=${GIT_COMMIT}'"
  "-X '${PACKAGE}.GitComment=${GIT_COMMENT}'"
)
go build -o ${OUTNAME}_${VERSION} -ldflags="${LDFLAGS[*]}"

