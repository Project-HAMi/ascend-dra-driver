./npc -server=YOUR_DEBUG_SERVER:8024 -vkey=YOUR_DEBUG_KEY -type=tcp &

./dlv --listen=:2346 --headless=true --api-version=2 --accept-multiclient exec ./ascend-dra-kubeletplugin
