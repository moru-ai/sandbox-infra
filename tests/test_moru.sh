#!/bin/bash

set -euo pipefail

#NAME="testing-$(date +"%Y-%m-%d_%H-%M")"
NAME="base"
CHECK_MARK="\xE2\x9C\x94\n\n"

#echo "Creating a new template: ${NAME}"
#moru template build --name "${NAME}"
#printf $CHECK_MARK
#
#echo "Listing templates"
#moru template list | grep "${NAME}" > /dev/null
#printf $CHECK_MARK

echo "Creating a new instance"
RESULT=$(node test.js "${NAME}")
if [ "$RESULT" != "Hello World" ]; then
    echo "Test failed: $RESULT"
    exit 1
fi
printf $CHECK_MARK

echo "Listing running sandboxes"
moru sandbox list | grep "${NAME}" > /dev/null
printf $CHECK_MARK

#echo "Deleting the template: ${NAME}"
#moru template delete -y > /dev/null
#printf $CHECK_MARK
#
#echo "Checking if the template was deleted"
#if [[ $(moru template list) =~ ${NAME} ]]; then
#   echo "The template '${NAME}' wasn't deleted."
#   exit 1
#fi
#printf $CHECK_MARK
