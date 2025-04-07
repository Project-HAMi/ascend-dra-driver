./npc -server=YOUR_DEBUG_SERVER:8024 -vkey=YOUR_DEBUG_KEY -type=tcp &

./dlv exec kube-scheduler --headless -l :2346 --api-version=2 -- --authentication-kubeconfig=/etc/kubernetes/scheduler.conf --authorization-kubeconfig=/etc/kubernetes/scheduler.conf --bind-address=127.0.0.1 --feature-gates=DynamicResourceAllocation=true --kubeconfig=/etc/kubernetes/scheduler.conf --leader-elect=false --v=1 --leader-elect-lease-duration="99999s" --leader-elect-renew-deadline="9999s"
