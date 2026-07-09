# Copyright 2022 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

GOLANG_VERSION ?= 1.26.0
LIBVNPU_BUILD_IMAGE ?= ascendai/cann:9.0.0-devel

DRIVER_NAME := ascend-dra-driver
MODULE := .

VERSION  ?= v0.1.0
vVERSION := v$(VERSION:v%=%)
REVISION ?= unknown
BUILD_DATE ?= $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')
VERSION_PACKAGE := github.com/Project-HAMi/hami-dra-driver/pkg/version

VENDOR := project-hami.io
APIS := npu/v1alpha1

PLURAL_EXCEPTIONS  = DeviceClassParameters:DeviceClassParameters
PLURAL_EXCEPTIONS += NpuConfig:NpuConfig

ifeq ($(IMAGE_NAME),)
REGISTRY ?= registry.example.com
IMAGE_NAME = $(REGISTRY)/$(DRIVER_NAME)
endif
