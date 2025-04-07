CGO_LDFLAGS_ALLOW='-Wl,--unresolved-symbols=ignore-in-object-files' go build -buildvcs=false -gcflags="all=-N -l" -o ../tools/ascend-dra-kubeletplugin ../../cmd/ascend-dra-kubeletplugin
