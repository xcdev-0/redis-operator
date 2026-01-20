#!/bin/bash
set -e

kubectl apply --server-side=true -k config/default
kubectl apply -f test-redis-cluster.yaml