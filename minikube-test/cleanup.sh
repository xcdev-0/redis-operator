# build-and-load.sh
#!/bin/bash
set -e  # 에러 발생시 중단

cd ../

# kubectl delete rediscluster test-redis-cluster 
# kubectl delete -k config/default 

IMAGE_NAME="redis-operator:latest"

echo "🗑️  로컬 Docker의 기존 이미지 삭제 중..."
docker images | grep redis-operator | awk '{print $3}' | xargs docker rmi -f 2>/dev/null || true


echo "🗑️  미니쿠베 내부의 기존 이미지 삭제 중..."
if ! minikube ssh "docker images | grep redis-operator | awk '{print \$3}' | xargs -r docker rmi -f" 2>&1; then
    echo "⚠️  미니쿠베 이미지 삭제 실패 (이미지가 없거나 에러 발생, 무시하고 계속)"
fi