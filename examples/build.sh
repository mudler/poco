#!/bin/bash

docker build -t firefox:latest firefox/

export CGO_ENABLED=0 

poco bundle --local --image firefox:latest --output firefox \
                --entrypoint /usr/bin/firefox \
                --app-mounts /sys \
                --app-mounts /tmp \
                --app-mounts /run