# build-and-load.sh
#!/bin/bash
set -e  # 에러 발생시 중단

cd ../

# kubectl delete rediscluster test-redis-cluster 
# kubectl delete -k config/default 

IMAGE=192.168.0.51:5000/redis-operator:dev

echo "🗑️  로컬 Docker의 기존 이미지 삭제 중..."
docker images | grep redis-operator | awk '{print $3}' | xargs docker rmi -f 2>/dev/null || true

