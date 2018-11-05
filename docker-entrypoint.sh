#!/bin/ash
set -e

if [[ -z "$FIM_CA" || -z "$FIM_CERT" || -z "$FIM_KEY" ]]; then
	/fimcontroller -insecure
else
	/fimcontroller -ca="$FIM_CA" -cert="$FIM_CERT" -key="$FIM_KEY"
fi
