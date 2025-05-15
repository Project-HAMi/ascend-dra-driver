#!/usr/bin/env bash

helm upgrade -i \
  --create-namespace \
  --namespace ascend-dra-driver \
  ascend-dra-driver \
  deployments/helm/ascend-dra-driver