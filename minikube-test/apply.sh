#!/bin/bash
set -e

cd ../

kubectl apply --server-side=true -k config/default
kubectl apply -f test-redis-cluster.yaml