#!/bin/bash

BLUE='\e[34m'
GREEN='\e[32m'
RED='\e[31m'
RESET='\e[0m'

timestamp=$(date +%y%m.%d.%H%M)

#set relative project root path
projectDir=`pwd`

declare -a arr=(
    "user-ms"
    "notification-ms"
    "billing-ms"
    "api-gateway"
    "frontend"
    )

count=1

for i in "${arr[@]}"
do
    echo -e "${BLUE}[$count/${#arr[@]}] Building $i$RESET"
    docker build --file "$projectDir/$i/Dockerfile" --tag "$i-local" "$projectDir/$i/"
    EXITCODE=$?
    if [ $EXITCODE != 0 ]; then
        echo -e "${RED}Build failed with exit code $EXITCODE$RESET"
        exit
    fi
    ((count++))
done
echo -e "${GREEN}[DONE] Building service images"

docker compose -p "user-bills" up --remove-orphans