#!/bin/bash

oc process -f generic-template.yaml NAME=jenkins NAMESPACE=myproject DOCKER_IMAGE=openshift/jenkins-2-centos7:latest | oc apply -f -
oc rollout latest dc/jenkins
